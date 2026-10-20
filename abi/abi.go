// Package abi embeds the contract ABIs (see README.md for their provenance).
package abi

import (
	"embed"
	"fmt"
	"sync"

	gethabi "github.com/ethereum/go-ethereum/accounts/abi"
)

//go:embed *.json
var files embed.FS

// Names of the embedded ABIs.
const (
	Transfer    = "GnoICS20Transfer"
	Admin       = "IBCAdmin"
	AdminExec   = "IBCAdminExec"
	LightClient = "GnoLightClient"
	Voucher     = "GnoVoucher"
	Router      = "ICS26Router"
)

var (
	mu     sync.Mutex
	parsed = map[string]*gethabi.ABI{}
)

// Load parses an embedded ABI once.
func Load(name string) (*gethabi.ABI, error) {
	mu.Lock()
	defer mu.Unlock()
	if a, ok := parsed[name]; ok {
		return a, nil
	}
	b, err := files.ReadFile(name + ".json")
	if err != nil {
		return nil, fmt.Errorf("abi %s: %w", name, err)
	}
	a, err := gethabi.JSON(bytesReader(b))
	if err != nil {
		return nil, fmt.Errorf("abi %s: %w", name, err)
	}
	parsed[name] = &a
	return &a, nil
}

// MustLoad is Load for package-level variables.
func MustLoad(name string) *gethabi.ABI {
	a, err := Load(name)
	if err != nil {
		panic(err)
	}
	return a
}
