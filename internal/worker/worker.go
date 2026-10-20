// Package worker is the T5 watch/check worker (docs/design.md §9): code-hash
// comparison of upgrade targets, the reproducible-build verification in
// Docker, storage-layout diffs and the deployment-manifest check.
//
// Phase 1 implements the code-hash job and the job bookkeeping; the Docker
// jobs are stubs that report what they would run.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"

	"github.com/allinbits/ibc-monitoring/internal/ethrpc"
	"github.com/allinbits/ibc-monitoring/internal/model"
	"github.com/allinbits/ibc-monitoring/internal/redact"
)

// JobKind names a job.
type JobKind string

const (
	JobCodeHash    JobKind = "codehash"
	JobBuildVerify JobKind = "buildverify"
	JobLayoutDiff  JobKind = "layoutdiff"
	JobManifest    JobKind = "manifest"
)

// Job is one run with its verdict.
type Job struct {
	ID         int               `json:"id"`
	Kind       JobKind           `json:"kind"`
	Subject    string            `json:"subject"` // e.g. "decision 1/7 kind 7", "recovery <hash>"
	Status     string            `json:"status"`  // queued | running | passed | failed | error | unsupported
	StartedAt  time.Time         `json:"started_at"`
	FinishedAt time.Time         `json:"finished_at,omitempty"`
	Verdict    string            `json:"verdict,omitempty"`
	Checks     []JobCheck        `json:"checks"`
	Log        string            `json:"log,omitempty"`
	Provenance *model.Provenance `json:"provenance,omitempty"`
}

// JobCheck is one comparison inside a job.
type JobCheck struct {
	Name     string `json:"name"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Passed   bool   `json:"passed"`
	Note     string `json:"note,omitempty"`
}

// Worker keeps the jobs.
type Worker struct {
	Eth *ethrpc.Client
	Log *slog.Logger

	mu   sync.Mutex
	jobs []Job
	next int
}

// Jobs lists every job, newest first.
func (w *Worker) Jobs() any {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]Job, len(w.jobs))
	copy(out, w.jobs)
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out
}

// sanitize redacts every text a job stores (docs/design.md §10.2).
func sanitize(j *Job) {
	j.Subject = redact.String(j.Subject)
	j.Verdict = redact.String(j.Verdict)
	j.Log = redact.String(j.Log)
	for i := range j.Checks {
		j.Checks[i].Note = redact.String(j.Checks[i].Note)
		j.Checks[i].Expected = redact.String(j.Checks[i].Expected)
		j.Checks[i].Actual = redact.String(j.Checks[i].Actual)
	}
}

func (w *Worker) add(j Job) Job {
	sanitize(&j)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.next++
	j.ID = w.next
	w.jobs = append(w.jobs, j)
	return j
}

func (w *Worker) update(j Job) {
	sanitize(&j)
	w.mu.Lock()
	defer w.mu.Unlock()
	for i := range w.jobs {
		if w.jobs[i].ID == j.ID {
			w.jobs[i] = j
			return
		}
	}
}

// CodeHashTarget is a candidate named by a decision or a recovery proposal.
type CodeHashTarget struct {
	Subject  string
	Address  common.Address
	Expected common.Hash
	// ManifestHash is the deployment manifest's hash for this component,
	// when the component is known (zero otherwise).
	ManifestHash common.Hash
}

// CodeHash compares the live code hash of a candidate with the pinned hash
// (and the manifest's) at the pinned block, and checks it is code rather
// than an EIP-7702 delegation designator (docs/design.md §9.1).
func (w *Worker) CodeHash(ctx context.Context, t CodeHashTarget, block uint64, prov model.Provenance) Job {
	j := w.add(Job{Kind: JobCodeHash, Subject: t.Subject, Status: "running", StartedAt: time.Now(), Checks: []JobCheck{}, Provenance: &prov})
	hash, size, err := w.Eth.CodeHash(ctx, t.Address, block)
	if err != nil {
		j.Status, j.Verdict, j.FinishedAt = "error", redact.String(err.Error()), time.Now()
		w.update(j)
		return j
	}
	passed := true
	code := JobCheck{Name: "address holds code", Actual: fmt.Sprintf("%d bytes", size), Passed: size > 0}
	if size == 0 {
		code.Note = "no code at the candidate address"
	}
	passed = passed && code.Passed
	j.Checks = append(j.Checks, code)
	designator := JobCheck{Name: "not an EIP-7702 delegation designator", Passed: true}
	if size == 23 {
		if c, err := w.Eth.Code(ctx, t.Address, block); err == nil && len(c) == 23 && c[0] == 0xef && c[1] == 0x01 && c[2] == 0x00 {
			designator.Passed = false
			designator.Note = "the address is a delegation designator; governance refuses it"
		}
	}
	passed = passed && designator.Passed
	j.Checks = append(j.Checks, designator)
	pin := JobCheck{Name: "live code hash equals the pinned hash", Expected: t.Expected.Hex(), Actual: hash.Hex(), Passed: hash == t.Expected}
	passed = passed && pin.Passed
	j.Checks = append(j.Checks, pin)
	if t.ManifestHash != (common.Hash{}) {
		mc := JobCheck{Name: "live code hash equals the manifest", Expected: t.ManifestHash.Hex(), Actual: hash.Hex(), Passed: hash == t.ManifestHash}
		passed = passed && mc.Passed
		j.Checks = append(j.Checks, mc)
	}
	j.FinishedAt = time.Now()
	if passed {
		j.Status, j.Verdict = "passed", "the candidate's code matches its pinned hash"
	} else {
		j.Status, j.Verdict = "failed", "the candidate does not match; a failing pending candidate is cancelled by kind 1 (ADR §7.6)"
	}
	w.update(j)
	return j
}

// BuildVerify is the reproducible-build verification: it will run
// scripts/verify-codehashes.sh of gno-eth-ibc at the named revision inside the
// Foundry image with CANDIDATES_ONLY or COMPONENTS (docs/design.md §9.1, §9.3).
// TODO(phase 5): implement the Docker runner; today it records an
// "unsupported" job that says what it would run. A job never records the
// environment's values (ETH_RPC_URL carries credentials), only the names.
func (w *Worker) BuildVerify(_ context.Context, subject, revision string, env map[string]string) Job {
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}
	sort.Strings(names)
	j := w.add(Job{Kind: JobBuildVerify, Subject: subject, Status: "unsupported", StartedAt: time.Now(), FinishedAt: time.Now(), Checks: []JobCheck{}})
	j.Verdict = fmt.Sprintf("not implemented: would run verify-codehashes.sh from gno-eth-ibc@%s in Docker with %s set", revision, strings.Join(names, ", "))
	w.update(j)
	return j
}

// LayoutDiff diffs `forge inspect <Contract> storage-layout` between the live
// component's revision and the candidate's (docs/design.md §9.1).
// TODO(phase 5).
func (w *Worker) LayoutDiff(_ context.Context, subject, contract, fromRev, toRev string) Job {
	j := w.add(Job{Kind: JobLayoutDiff, Subject: subject, Status: "unsupported", StartedAt: time.Now(), FinishedAt: time.Now(), Checks: []JobCheck{}})
	j.Verdict = fmt.Sprintf("not implemented: would diff the storage layout of %s between %s and %s", contract, fromRev, toRev)
	w.update(j)
	return j
}
