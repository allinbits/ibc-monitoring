// Definitions adapted from the monitoring glossary; keep all help copy here.
export const glossary: Record<string, string> = {
  "base units":
    "Every amount is an exact integer in the token's smallest unit (uint256, sent as a decimal string). Decimals change only how the console displays it, never the value.",
  "unix seconds":
    'Times such as meter at, enrolled, decision made/recorded/due, flag at, and quarantine_end are seconds since 1970-01-01 UTC. 0 means "none" or "not yet".',
  "seconds vs nanoseconds":
    "trusting_period_ns is in nanoseconds. Quarantine, recovery delay, silence and hold period are in seconds; the console divides some of them by 86,400 to show days.",
  "height / block / slot":
    "A gno.land height is a tm2 block number. An Ethereum block is an execution block number. A slot is a beacon-chain slot (12 s), which is what gno.land's Ethereum client counts its head in.",
  collection:
    "The monitor's sequence number for one read of the chains; every snapshot records the collection it came from.",
  "height (gno.land pin)":
    "The committed gno.land block the latest collection read every value at. Both RPC providers must agree on its header.",
  "block (ethereum pin)":
    "The finalized Ethereum execution block the latest collection read every value at. Every eth_call is pinned to it, never to latest.",
  "block hash / hash":
    "The pinned block’s hash. Ethereum’s hash is recomputed from the decoded header instead of trusting the node’s reported hash.",
  time: "The pinned block's timestamp.",
  "header agreement":
    "How the pinned header was authenticated: cross-checked (the second provider agreed), light-client (the primary is a light client), provider (primary only, not checked yet) or unverified (disagreement or no second provider; nothing from this chain is evaluated).",
  "cross-check note":
    "The cross-check's result in words. \"finalized by the second provider\" means the second provider's own finalized head is at or past the pinned block, so finality was established independently.",
  "finality (pin)":
    "finalized for Ethereum (selected by the finality mode); committed for gno.land, where a committed height is final under tm2's rule.",
  "finality mode":
    "How the Ethereum pin is chosen: tag uses the node's finalized block; depth:N and latest are for local networks only and never support integrity containment.",
  "eth head age (~13 min)":
    "The pinned Ethereum block is the latest finalized one, which trails the chain head by about two epochs (around 13 minutes), so a head age of about 800 s is normal. It becomes stale only past max_head_age (default 45 min).",
  "heads (primary / second)":
    "Each provider's own reported head (gno.land: latest height; Ethereum: its own finalized block). age_seconds is the collection time minus the pin's block time.",
  "stale (pin)":
    "The pin failed a freshness bound: head age (gno.land 2 min, Ethereum 45 min), provider lag (the second provider more than 20 / 64 blocks ahead) or no progress for 10 min. A stale pin is not evaluated.",
  "collection incomplete":
    "The chain was read, but a cross-chain input was missing (an Ethereum collection without a fresh gno.land discovery). Its clients and governance are still fresh; its route snapshots are not.",
  "collection failed / n times in a row":
    "The chain could not be read, or did not finish within its timeout. The last good snapshots are kept and marked stale.",
  "last success": "The time of this chain's latest successful, header-agreed collection.",
  counts:
    "How many gno.land clients, Ethereum verifiers (GnoLightClients) and routes the latest collection found.",
  nodes: "The configured names of the primary gno.land and Ethereum providers.",
  "provenance badge":
    "Which chain, height or block, and header verification a value was read at, and whether it is stale.",
  height: "The gno.land height or Ethereum block number the value was read at.",
  "block hash": "The hash of that block.",
  node: "The provider that answered the read. It is empty on a derived input such as the gno.land discovery used by the relationship check.",
  "read at": "When the monitor made the read. It is zero on a derived input.",
  "verification / header verification":
    "The header agreement of the block the value was read at (see Header agreement).",
  finality: "finalized (Ethereum) or committed (gno.land). It is empty on a derived input.",
  "state verification / value verification":
    "Whether the value itself was authenticated: proof (a storage or store proof against the agreed header), agreed (the same value read from both providers) or none (one provider's answer). Integrity checks may act only on proof or agreed.",
  stale:
    "The snapshot does not come from its chain's latest successful, header-agreed collection. It is shown greyed out, is not evaluated, and keeps its alarms as they were.",
  unevaluated: "Subjects a check skipped because their snapshot was not fresh.",
  "client_expiry/gno":
    "A liveness check of every IBC client on gno.land: its status, how long until it expires, and whether its head is too old. It only raises alarms and never sets a flag.",
  "client_expiry/ethereum":
    "The same check for the GnoLightClients on Ethereum (the route's current client, kind 6 targets, and the candidate of an open recovery).",
  route_relationship:
    "A cross-chain check that the gno.land route names Ethereum's current route client as its counterparty. A mismatch is a warning, typically a replacement or recovery that one chain executed before the other.",
  "planned checks":
    "closed_pair, supply_identity, orphan_receives, proof_heights, header_record, foreign_decisions, liveness_* and gov_ledger are designed (design §7) but not registered in this build.",
  "check status":
    "ok (evaluated, nothing alarming), alarm (a finding raised an alarm), skipped (nothing fresh to evaluate; retried) or error (failed; counted like a skip).",
  findings:
    'How many subjects the check judged. "alarming" counts the findings that carry an alarm key.',
  "skipped n in a row":
    "Consecutive evaluations that were skipped or errored. At deferred_alarm_failures (default 6), the check's _unavailable alarm is raised.",
  reason: "Why the check skipped, or which subjects it left unevaluated.",
  "last run": "When the check's latest evaluation finished.",
  "head / head_age_s / time_left_s / trusting_s":
    "client_expiry values: the client's latest height (a slot for gno.land's Ethereum client), the age of the head in seconds, the seconds until derived expiry, and the trusting period in seconds.",
  "client expiry thresholds":
    "The client is warning when expiry is within 72 h, or when the head is older than 24 h (gno.land Ethereum client) or 72 h (GnoLightClient, Tendermint). It is critical within 12 h of expiry, or when frozen or expired.",
  alarm:
    "A deduplicated condition, keyed by its alarm key. It is raised once, updated while the condition holds, and cleared only by a later evaluation of its subject that no longer finds it.",
  "alarm key":
    "The alarm's identity, e.g. client_expiry/gno/ethereum-1, route_relationship/client-0, collection_unavailable/ethereum, verification_failing/gno, head_stale/ethereum or <check>_unavailable.",
  severity:
    "info (no alarm), warning, critical, or break (an integrity break; the only severity eligible for auto-flagging).",
  state: "raised (standing) or cleared. The model also defines acknowledged.",
  "event type raised / updated / cleared":
    "Each transition is sent to the sinks. updated means the condition still holds with new evidence or a new count; cleared never triggers anything.",
  "first seen / since": "When the alarm was raised in its current generation.",
  "last seen": "The alarm's latest transition.",
  generation: "Increments each time the alarm key is raised again after a clear (a new incident).",
  transitions:
    "A running count of the alarm's transitions across generations. Receivers use it as replay protection.",
  'deferred alarm ("could not evaluate n times in a row")':
    "<check>_unavailable (warning), raised when a check skipped or errored N consecutive times (default 6). The detail gives the last skip reason.",
  "collection_unavailable/<chain>": "Critical: the chain could not be read N consecutive times.",
  "verification_failing/<chain>":
    "Critical: the chain's header did not agree between the providers N consecutive times.",
  "head_stale/<chain>":
    "Critical: the collections pinned a stale head N consecutive times. This is a liveness finding and is never eligible for auto-flagging.",
  evidence: "The provenance and values of the finding that raised or updated the alarm.",
  "auto-flag eligible":
    "The alarm is a break from an integrity check, so the auto-flag sink would act on it.",
  "autoflag requested / effective":
    'Requested is the configuration (off, dry-run, on). Effective is what actually runs: unavailable while the sink is not built, so the console never claims "on" unless it is.',
  "autoflag (watch keys)":
    "The sink that, on a fresh, authenticated break, has a watch key (seat 4 or 5) set containment flags. It never clears a flag, never sets a sends flag and never votes.",
  client:
    "An IBC light client: on gno.land, an Ethereum client (ethereum) or a tendermint client; on Ethereum, a GnoLightClient contract registered with the router under a client id.",
  role: "Why an Ethereum verifier is listed: route (the route's current client), enrolled (a kind 6 target) or candidate (an open recovery's verifier).",
  status:
    "As the chain reports it: Active, Frozen, Expired, Unknown or Unauthorized (gno.land core). For a GnoLightClient it is derived from isFrozen and isExpired. Only an Active client verifies proofs.",
  frozen:
    "Misbehaviour froze the client. It proves nothing until it is recovered or the route is replaced.",
  "retired for transfer / gov":
    "gno.land core no longer accepts this client on the transfer or gov port, because a replacement retired it.",
  "latest height (gno.land ethereum client)":
    "The beacon slot of the client's latest trusted header, not an execution block number.",
  "exec block": "The Ethereum execution block number of that latest trusted header.",
  "latest height (gnolightclient)":
    "The gno.land block height of the latest header the Ethereum-side client accepted.",
  "head time": "The timestamp of the latest trusted consensus state.",
  "trusting period":
    "How long a head stays trusted without an update. The gno.land Ethereum client uses 8 sync-committee periods (786,432 s, about 9.1 days); a GnoLightClient's period is set in its client state (14 days here).",
  expiry:
    "Derived expiry. For a gno.land Ethereum client, it is the start of slot latest + 8 sync-committee periods + 1 on the slot clock, rather than head time plus a period. GnoLightClient and Tendermint clients expire at head time plus the trusting period.",
  "verifier version": "The verifier code a gno.land client runs (e.g. ethereum/v0).",
  "trust generation":
    "A counter bumped by a recovery, freeze, upgrade, migration or substitution of a client. Outcomes proven under an earlier generation are void.",
  counterparty: "The client id on the other chain that this client pairs with.",
  "chain id":
    "The chain the client follows: for a gno.land Ethereum client, the Ethereum chain id (e.g. 11155111); for a GnoLightClient, gno.land's chain id.",
  "route key": "The route's stable identifier: the first client id it had. Later clients alias it.",
  replaced: "Earlier clients that route replacements retired, oldest first.",
  enrolled: "When the route was enrolled (unix seconds).",
  activated: "The gno.land height at which the route's controls were activated.",
  closed:
    "A route replacement closed the route. Nothing new is credited, sent or paid, and only governed settlements pay refunds, until a governed reopening.",
  "loss-closed":
    "Some denom of the route has an unresolved loss, so the route takes no new exposure (no gno-native send, credit, claim payout or sweep payment). Voucher returns, acks, timeouts and refunds continue.",
  "fee collector":
    "The gno address that receives the route's send fees and claim fees. It is never a bridge realm.",
  "refused / protected recipients":
    "Ethereum addresses that gno.land sends may not target. Protected ones (the router, ICS20, IBCAdmin and the voucher tokens) can never be unrefused.",
  "unrecovered / unresolved (route)":
    "Saturating sums of the denoms' unrecovered and unresolved losses. The denoms can have different units, so the sum is a signal (zero or not) rather than an amount.",
  denom:
    "The accounting denom: the escrow denom for unescrow (e.g. ugnot), or the voucher's ibc/… denom for mint.",
  "kind unescrow":
    "A gno-native asset. Sends escrow it on gno.land; returns from Ethereum release it from that escrow, and the escrow (the lock) caps payouts.",
  "kind mint":
    "An Ethereum-native asset. Receives mint gno.land vouchers and sends burn them. With no lock, it has an absolute day cap and a supply cap.",
  token:
    "The denom's ERC20 on Ethereum. For unescrow it is remote (its GnoVoucher); for mint it is the base of the voucher's denom trace.",
  remote:
    "The lowercase Ethereum address of an unescrowed denom's voucher token on Ethereum. Sends to it are refused. It is empty for mint.",
  trace: "A minted denom's trace path (transfer/<client>), which must name a client of the route.",
  decimals:
    "The voucher's governed decimals (0–18), set only for a mint denom and fixed once the voucher exists. It is always 0 for unescrow, because a gno-native token takes none. Amounts stay in base units either way.",
  "display precision vs decimals":
    "ugnot shows decimals 0 because it is gno-native, but the console displays it at GNOT's native 6 decimals. A minted denom displays at its governed decimals (e.g. 18 for the test ERC20).",
  "cap floor":
    "For unescrow, the floor of the day cap, which is max(cap_floor, lock/10). For mint, the day cap itself. Lowering it is immediate but never goes below the launch cap floor; raising it waits 7 days.",
  "launch cap floor":
    "The cap floor the denom was enabled with, the lowest the cap floor can ever be set.",
  "day cap (gno.land)":
    "The bridge meter's ceiling and daily refill. It is max(cap_floor, lock/10) for unescrow, recomputed at every refill, and cap_floor for mint.",
  "supply cap":
    "For mint only, the bound on what is booked against the voucher: outstanding + returning + proven owed + owed + withheld + swept. A receive beyond it is refused with an error ack. It is 0 for unescrow.",
  fee: "For unescrow, charged on top of every send. For every denom, it is the first installment of every claim, paid to the fee collector. Lowering it is immediate; raising it waits 7 days.",
  "fee floor":
    "The least the fee may be. Any change to it waits 7 days, and the fee must stay at or below cap_floor/4.",
  "send max / send min (gno.land)":
    "For unescrow, a send must be between them (0 < min <= max). For mint both are 0, meaning not applicable: voucher returns are never limited.",
  "refund cap (gno.land)": "The refund meter's ceiling and daily refill, in base units per day.",
  "bridge meter (payout meter)":
    "The token bucket that limits claim payouts of the denom. Its level is the capacity still available to pay out: it refills toward the day cap at day cap/86,400 per second, and payouts take from it.",
  "stored level":
    "The bridge or refund meter's level as of its last refill (at), not as of now. Time since then will have refilled it further, up to the ceiling.",
  at: "The unix time of the stored level (the meter's last refill or payout).",
  "meter epoch":
    "The payouts epoch the bridge meter last saw. A new epoch (a cleared payouts flag, a client migration, or a route replacement) empties the meter, which then refills from zero.",
  "live level":
    'The level refilled to the snapshot time, shown only from an exact source (the realm\'s Reader.Meters). Otherwise "unavailable", because the refill depends on the epoch reset, a fractional remainder and the stored rate and ceiling, none of which the JSON exposes.',
  "recipient meter":
    "Each recipient's own bucket at a quarter of the day cap. A payout turn pays at most min(bridge, recipient, free lock). It is not rendered by the realm.",
  "meters start empty":
    "A bridge meter starts at 0 (nothing available) at launch and on every new payouts epoch. A refund meter starts at 0 when the denom is enabled and keeps its state across epochs and replacements.",
  "refund meter (gno.land)":
    "The token bucket that limits refunds of the denom. Its level is refund capacity still available, refilling at refund_cap/86,400 per second up to refund_cap. A refund is paid at once only if all of it fits; otherwise it queues.",
  "cap change":
    "A governed change first refills the meters up to now at the old cap, then applies the new cap. A higher ceiling does not raise the stored level.",
  claims:
    "Credits gno.land owes recipients for received transfers, one per packet. Anyone can pay them with Claim, round-robin by recipient and oldest first per recipient, under the meters.",
  waiting: "The number of recipients that have at least one open claim (a count, not an amount).",
  "claim state open": "Credited and unpaid or partly paid; it is in the payout queue.",
  "claim state withheld":
    "Voided as withheld: no longer paid, but its remainder stays booked and a 7-day restore can reopen it.",
  "claim state forged":
    "Voided as forged: the remainder is dropped, and whatever was already paid is booked as a loss. This is final.",
  "claim state done": "Paid in full.",
  "claim by packet / vote":
    "packet means booked by the received packet itself; vote means booked by a credit-by-vote decision, the manual fallback that replaces a sweep entry.",
  "claim amount / paid / reduced / fee":
    "Amount is what the claim owes in total, already net of reductions. Paid is what has been paid, fee included. Reduced is what liability reductions cut. Fee is the fee locked at credit; payouts use the lower of it and today's fee.",
  "claim at / height / seq":
    "The credit time (unix seconds), the gno.land credit height, and the claim's position in its recipient's queue.",
  sweeps:
    "Credits booked as owed but paid to nobody until a 7-day sweep decision directs them. These are amounts below the fee, amounts above the free escrow (unescrow), and receivers that are a bridge realm or an invalid address.",
  "sweep reason":
    'Why the credit was swept, e.g. "below the fee", "above the free lock" or "bridge realm".',
  "sweep voided / paid / paid to":
    'Voided means voided as forged (final). Paid means paid by a sweep decision, or replaced by a credit by vote (paid_to "credit by vote"). Paid to is the decision\'s payee.',
  "from claim / claim paid":
    "The entry was an open claim moved here because its recipient became a bridge realm; claim paid is what that claim had already paid out.",
  "send record":
    "The immutable per-packet record written with every gno.land send: packet, commitment, sender, receiver, amount and fee. The ack or timeout attaches only the outcome, processing height and payment state.",
  'outcome ""': "Pending: no ack or timeout has been processed yet.",
  "outcome success": "A success ack was processed: delivered, nothing owed.",
  "outcome error / timeout":
    "An error ack, or a timeout, was processed: the amount (not the fee) is owed back to the sender.",
  'payment ""': "Nothing owed: the send is in flight or was acknowledged as a success.",
  "payment paid": "Refunded at once, when the ack or timeout was processed.",
  "payment queued":
    "A refund is owed and waiting: under the refund meter, a refunds hold, a closed route, short escrow, or parked by a settlement.",
  "payment claimed": "A queued refund later paid in full by ClaimRefunds.",
  "payment rejected":
    "A queued refund that a reconciliation settled unpaid, because the packet was delivered.",
  "payment reconciled": "A refund that a reconciliation (settlement) paid by decision.",
  settlement:
    "The governed reconciliation's action on the record. 0: unsettled. 1: genuine refund paid or owed. 2: queued refund rejected (delivered). 3: delivered, nothing owed. 4: forged refund; what was paid is a loss. 5: forged receive, an action on inbound sequences.",
  proven:
    "The destination outcome proven through the replacement client. 0: none. 1: delivered. 2: refundable (no receipt, or an error ack). 3: collision (another packet's receipt at the sequence, so refundable). A settlement's action must match it.",
  "proven at / by / gen":
    "The proof's height on the replacement client, the client that verified it, and that client's trust generation then. The proof is void once the client is no longer Active or its generation changes.",
  reservation:
    "For mint, the burned supply this send still holds against the supply cap, released on success or consumed by a refund.",
  "open record":
    "A record with settlement 0 whose outcome is pending or whose payment is queued. Open records of a retired client block reopening.",
  credited:
    "Lifetime amount booked as claims, by packet or by vote. Swept credits are not included. A lifetime statistic.",
  owed: "The remainders of open claims, which are still payable.",
  withheld: "The remainders of withheld claims: still booked and restorable, but not paid.",
  swept: "The amounts of live (unpaid, unvoided) sweep entries.",
  paid: "Lifetime claim installments paid to recipients, fees excluded.",
  fees: "Lifetime claim fee installments paid to the fee collector. Send fees are counted separately.",
  "sweep paid": "Lifetime sweep entries paid by sweep decisions.",
  outstanding:
    "For mint only, the vouchers this route has in circulation by its own books. It starts at the vouchers already circulating when the denom was enabled; it rises with every claim payout (fee included), sweep payment and refund re-mint, and falls by the amount of every recorded send (floored at 0). It is always 0 for unescrow, and it is not the token's own total supply.",
  returning:
    "For mint only, vouchers burned by sends that are not settled yet. They are still reserved against the supply cap, because a refund would mint them again.",
  "proven owed":
    "Refunds proven owed (refundable or collision) on sends a forged success released, or reserved by a replacement's preparation. They are held until their settlement.",
  "in flight":
    "What open send records may still owe: their amounts less what was refunded. Records with a queued refund are included, so this overlaps queued refunds.",
  sent: "Lifetime amount escrowed (unescrow) or burned (mint) by recorded sends, fees excluded.",
  "send fees": "Lifetime fees charged on sends. Only unescrow sends pay a fee.",
  refunded: "Lifetime refunds paid, whether immediately, by ClaimRefunds or by settlement.",
  "queued refunds (amount)":
    "Refunds owed and not paid yet, including settlement refunds parked for lack of free escrow.",
  "queued refunds (count)": "The number of entries in the denom's refund queue.",
  lost: "Lifetime losses booked in the loss ledger: forged payouts, forged refunds, forged receives and recorded losses. A statistic.",
  recapitalized: "Lifetime recapitalizations paid into escrow against a loss incident.",
  reduced:
    "Lifetime liability reductions: published haircuts on open, withheld or swept claims of an incident.",
  accepted:
    "Lifetime losses accepted as a deficit (by decision or at reopening). An accepted loss stays unrecovered.",
  unrecovered:
    "Open losses not yet recapitalized or reduced. This is the loss term of the supply identity; accepted deficits stay in it.",
  unresolved:
    "The part of unrecovered losses that has not been accepted as a deficit either. While it is above 0 the route is loss-closed.",
  "lock / escrow accounting":
    "The transfer realm's escrow accounting (total_escrow) of the denom for this route, in base units: what the route's sends escrowed less what was released. It is not a physical balance, and it is 0 for mint.",
  "free escrow (free lock)":
    "The lock less Reserved(): open and withheld claim remainders, queued refunds and proven reservations. A credit above it is swept.",
  "physical escrow balance (gno.land)":
    "The transfer realm's actual bank or GRC20 balance. A single account serves every client of the port, so its coverage is checked against the sum of all clients' obligations.",
  "loss ledger":
    "Losses keyed by packet direction (recv/send) and key, with amount, incident and cause.",
  "loss chain":
    "gno means the loss is authoritative on gno.land. ethereum is a mirror of an Ethereum-side loss: it closes the route to new exposure but is resolved only by an accepted deficit.",
  "route key / client (ethereum)":
    "The route's stable key on Ethereum and its current router client id there, read from Ethereum itself.",
  migrations:
    "The route's client generations from Ethereum's history (GnoRouteMigrated): old id, new id and the block where it happened.",
  escrow:
    "The route's escrow contract on Ethereum, which holds Ethereum-native tokens sent to gno.land.",
  "ethereum escrow balance":
    "The route escrow's physical balanceOf of an Ethereum-native token (origin ethereum) at the pinned block. It backs the gno.land vouchers of that token.",
  "voucher supply":
    "The exact total supply of a minted denom's gno.land voucher. Ethereum's escrow balance of the token backs it.",
  "closed (ethereum)":
    "A replacement (kind 10 or emergency recovery) or a late loss closed the route. Escrowed sends and new receives stop; voucher returns still exit, and refunds wait for reopening except those kind 12 pays.",
  reopening:
    "A kind 13 is under way, accepting the route's losses as deficits in batches (continueReopen). The route opens after the last batch.",
  paused:
    "At least one standing route flag (any seat). Receives are error-acked and escrowed sends refused; voucher burns, acks, timeouts and refunds continue.",
  "refunds held":
    "At least one standing refunds flag. Proof-based refunds, claim/claimParked payouts and refund-paying kind 12 wait. Nothing is cancelled.",
  "recovery blocked":
    "An emergency recovery is open, so every token receive on the route is refused until it closes.",
  incomplete:
    "The Ethereum route snapshot lacks a cross-chain input (a fresh gno.land discovery), so checks that depend on it defer.",
  "route flag (pause)":
    "A seat's flag on the route. One standing flag pauses receives and escrowed sends at once. It stands only while the key that set it still holds its seat.",
  "refunds flag":
    "A seat's flag dedicated to suspected verifier compromise. One standing flag holds the route's refunds.",
  "flag seat / key / time / block":
    "Which seat set it (0–3 guardians, 4–5 watch keys), the key, and the unix time and block of its last setting (refreshed on every set).",
  "route hold / refunds hold":
    "Two standing guardian flags in the same set (route or refunds). While a hold stands, an unpause (kind 2) of the route is spent. It ends 24 h after a clear takes it below two, or at once when a seat replacement retires one of the two flagging keys.",
  "hold since":
    "When the hold began (unix seconds). Its end is not stored on chain and is shown as unknown unless the flag history establishes it.",
  "quarantine (seconds)":
    "The route's quarantine duration in seconds (kind 11; 0 disables it). After each live validator-set transition of the GnoLightClient, token receives are error-acked for this long.",
  "quarantine end / deadline":
    "The unix time until which receives wait, covering transitions already seen and the client's latest transition. 0 means no quarantine has started; a past time means it has elapsed.",
  incident: "The 1-based number of the incident that covers this client. 0 while it is live.",
  "incident status": "1 open, 2 reopening (kind 13 under way), 3 resolved.",
  "suspect from":
    "The Ethereum block from which acks and timeouts processed through the retired client are suspect and must be settled by kind 12.",
  "incident client": "The keccak hash of the retired client id the incident covers.",
  "pending incidents":
    "Unresolved replacement incidents whose client still has an open or unsettled suspect record. Kind 13 waits for this to reach 0.",
  "resolved up to": "How many incidents, counting from the first, are resolved.",
  "limits set":
    'The token has limits on the route. Without them, its receives and escrowed sends are refused (the "no limits" pill).',
  "max per message":
    "The largest single receive (a voucher mint or escrow release) the route accepts. Larger receives are error-acked and refunded at the source.",
  "day cap (ethereum)":
    "The receive bucket's capacity: at most this much can be received at once, draining at day_cap/86,400 per second. 0 refuses every receive.",
  "receive level (live)":
    "Capacity used, not remaining: what was charged less what has drained since, read live from limits() at the pin. Available = day cap - receive level, and the bar shows the used share.",
  "refund cap (ethereum)": "The refund bucket's capacity per day. 0 queues every refund.",
  "refund level (live)":
    "Refund capacity used, not remaining: charged refunds less what has drained since. Available = refund cap - refund level, and the bucket starts with all of it available.",
  "send max / send min (ethereum)":
    "Bounds on escrowed sends of Ethereum-native tokens; voucher burns are never limited. 0 means unset: no maximum, or no minimum.",
  reserved:
    "Escrow of the token that receives and refunds may not release. It covers refunds owed (queued, parked, or decided but unpaid), held suspect successes, and outstanding escrowed sends whose refund could still be owed. Vouchers reserve nothing.",
  "losses (ethereum)":
    "Losses kind 12 recorded on the route for the token. Ethereum has no recapitalization or reduction, so this is also its unrecovered loss.",
  "accepted deficit":
    "The part of the losses that kind 13 accepted as a deficit at reopening. losses - accepted deficit is what still awaits acceptance.",
  "parked (ethereum payment)":
    "A refund whose token payment failed, or that would take another record's reserved backing. It waits apart and is retried by claimParked.",
  retired:
    "A replacement retired this client id from the route, so its records are settled by kind 12.",
  "open records":
    "The client's send records still open: a packet outstanding, or a refund owed (queued or parked) and not settled.",
  "suspect start":
    "The index, in the client's list of processed acks and timeouts, of the first record processed at or after the incident's suspect block.",
  "suspect open": "How many of the client's suspect records are still unsettled.",
  processed: "How many acks and timeouts this client id has processed.",
  "relationship agree":
    "The gno.land route names Ethereum's current route client as its counterparty, and the clients agree.",
  "gno.land disagrees":
    "A mismatch: no gno.land route names the Ethereum route, or the clients differ. Usually one chain executed a replacement or recovery the other has not processed yet.",
  deferred:
    "One of the two inputs (the Ethereum route or the gno.land discovery) was not fresh when last evaluated, so no verdict was drawn.",
  "gno discovery":
    "The gno.land collection whose clients and routes the cross-chain comparison used.",
  evaluated: "When the relationship check last judged this pair.",
  governor:
    "The governor key compiled into ibcgov. It decides through the same builders, checks and delays as GovDAO until it yields.",
  yielded:
    "The gno.land height at which the governor called Yield() (0 if it has not). After it, only GovDAO decides, permanently.",
  "govdao governs":
    "Whether GovDAO's proposals decide now: true after the yield, or from the start if no governor was compiled.",
  seats:
    "Six seats: 0–3 guardians (2 and 3 held by outsiders) and 4–5 watch keys (the operator's monitors). An empty seat cannot flag, and a seat change waits 7 days.",
  guardian:
    "A seat holder that can set flags. Two guardian seats flagging make a hold. Guardians delay but never veto.",
  "watch key":
    "A monitor-held seat that can set flags (the auto-flag sink signs with it). Its flags pause, but do not count toward a hold.",
  "flag (gno.land)":
    "A seat's flag on one function of a route (or of a client without a route). One flag pauses that function at once; it is cleared by its owner or by an unpause decision.",
  "flag function sends":
    "Pauses gno-native sends on the route. Voucher returns (burns) are never paused.",
  "flag function credits": "Pauses crediting receives from Ethereum.",
  "flag function payouts":
    "Pauses claim payouts and defers sweep payments. Clearing it starts a new payouts epoch (meters empty). It may name up to 64 suspect packets.",
  "flag function refunds":
    "The refunds hold: delays error-ack and timeout refunds, queued-refund payouts and refund-paying settlements, without cancelling them.",
  naming:
    "The suspect packet keys a guardian's payouts flag names. A void of only named packets applies immediately instead of after 7 days.",
  "hold (gno.land global)":
    "Two distinct guardian seats flagging anything, on any route or client. Every queued 7-day decision is deferred (seat changes land no sooner than day 21) and unpause decisions are spent.",
  "guardian seats (hold) / explained":
    "The number of distinct guardian seats the monitor saw flagging across all subjects. Explained means that count (2 or more) accounts for the chain's Held().",
  "epoch (gno.land)":
    "The governance channel's epoch on gno.land. It is opened by binding the channel and advanced by each route replacement or recovery; every Ethereum-bound decision is numbered within an epoch.",
  "epoch client":
    "The gno.land Ethereum client the current epoch's governance packets travel over.",
  "last seq (gno.land)": "The last Ethereum-bound decision number dispatched in the current epoch.",
  decision: "A governance action in ibcgov's ledger, numbered by id, with kind, text and note.",
  "decision state pending":
    "Recorded and not yet applied, waiting for its due time or for someone to Execute. A hold defers it.",
  "decision state applied": "Executed.",
  "decision state cancelled": "Withdrawn by a cancel decision before it applied.",
  "decision state spent":
    "Recorded with no effect: refused against the live state when recorded or executed, or an unpause under a hold.",
  "made / recorded / due / applied / cancelled":
    "Unix times: when the proposal was built, when the decision was recorded, when it may first be applied (recorded + 7 days for a queued change), when it applied, and when it was cancelled (0 if not).",
  note: "What happened at execution, in words. For an Ethereum-bound decision, the epoch and seq it was dispatched as.",
  dispatch:
    "The immutable governance packet (envelope version | epoch | seq | kind | params) that ibcgov sends to IBCAdmin for an Ethereum-bound decision.",
  "seq (dispatch)":
    "The decision's number on the governance channel within its epoch, the order IBCAdmin records in.",
  "kind (dispatch)":
    "The Ethereum decision kind: 1 cancel, 2 unpause, 3 seats, 4 limits, 5 voucher, 6 client, 7 upgrade, 8 role, 9 hand-over, 10 route replacement, 11 route settings (quarantine), 12 settle, 13 reopen.",
  "decision (dispatch)": "The ibcgov ledger id the dispatch was made for.",
  "answer / status":
    "IBCAdmin's acknowledgement status: 0 no answer yet, 1 recorded, 2 buffered (stored ahead of a gap, or halted), 3 duplicate, 4 conflict (a different payload for that seq, which flags IBCAdmin), 5 spent.",
  redispatch:
    "The channel asked for the same bytes to be sent again (an error ack, timeout, malformed ack or redispatch = 1). Anyone may call Redispatch, which reuses the seq and bytes in a fresh packet.",
  "in flight (dispatch)":
    "A packet for this dispatch is out and not yet acknowledged or timed out.",
  transport: "The gno.land core packet sequence of the last packet sent for this dispatch.",
  sends: "How many packets have been sent for this dispatch, including re-sends.",
  event: 'The last thing that happened to the dispatch, in words, e.g. "acknowledged: recorded".',
  envelope: "The dispatch's exact bytes (hex), which are immutable once bound.",
  "epoch (ethereum)":
    "IBCAdmin's current governance epoch. Opening a new one (route replacement or recovery) drops the old epoch's unexecuted decisions, buffer and tombstones.",
  "gov client":
    "The Ethereum router's client id for gno.land that the current epoch's governance arrives over.",
  "last seq (ethereum)": "The last decision number IBCAdmin has recorded in order in this epoch.",
  "gno chain":
    "The gno.land chain id IBCAdmin was deployed for; an emergency recovery's client must follow it.",
  "setup done":
    "finishSetup ran. Before it, nothing is recorded, executed or recovered, and IBCAdmin's flags wait.",
  "last record":
    "The block time of the last recorded decision, or of the epoch's opening. Channel silence for the recovery is measured from it.",
  "executor / code hash":
    "The IBCAdminExec contract IBCAdmin delegatecalls, and the code hash it must have. Only a kind 9 hand-over changes this pin.",
  "flags on ibcadmin":
    "Seat flags on IBCAdmin itself. Any standing flag defers every decision kind except unpause, seat changes, hand-over (which has its own bound) and revocations.",
  "conflict flag / conflict block":
    "The block of the latest conflict (two payloads for the same seq). It flags IBCAdmin like a guardian flag until a later unpause or a new epoch clears it; 0 means none.",
  "hold (ibcadmin)":
    "Two standing guardian flags on IBCAdmin. 7-day kinds are deferred, and a seat change or hand-over lands no sooner than day 21 of it.",
  "hold history":
    "Past IBCAdmin hold intervals (start to end, at most 8). A merged interval combines older holds and is never used as an anchor.",
  keys: "IBCAdmin's six seat keys: 0–3 guardians, 4–5 watch keys.",
  "decision state buffered (1)":
    "Received ahead of last seq and stored, waiting for the gap to fill or, if halted, for drain() through a working executor.",
  "decision state pending (2)":
    "Recorded and waiting for its delay. It may also be deferred by flags or holds, or suspended by an open recovery.",
  "decision state executed (3)": "Carried out.",
  "decision state spent (4)":
    "Closed with no effect: bad parameters, an unknown kind, or an executor refusal or revert.",
  "decision state cancelled (5)": "Cancelled by a kind 1 tombstone.",
  tombstoned:
    "A kind 1 cancel covers this decision number. A pending tombstoned decision is cancelled when anyone calls execute.",
  bad: "The envelope was over 1,024 bytes or was a malformed cancel. The decision is spent at recording.",
  "recorded / block":
    "When IBCAdmin recorded the decision (unix seconds), and in which Ethereum block.",
  "matures / mature at":
    "Derived as recorded time + the kind's delay (24 h for kind 2, 7 days otherwise); a kind 8 revocation is due at recording. Flags, holds or an open recovery can still defer execution past it.",
  "suspended by the open recovery":
    "An authority-changing kind waiting because an emergency recovery is open. It is due at once after the recovery closes.",
  "buffered sequences":
    "Decision numbers above last seq that are stored but not recorded. They indicate a gap, or a halted decision that holds back everything after it.",
  "recovery enabled":
    "Whether the guardian-proposed route replacement that bypasses the governance channel exists. It is fixed at deployment, and a delay of 0 disables it.",
  delay:
    "Seconds from a recovery proposal opening until anyone may execute it. Production requires at least 30 days.",
  "threshold (approvals)":
    "How many of the 4 guardian seats must approve the same proposal, while it is eligible, for it to open.",
  "cancel threshold": "How many guardian cancel votes cancel an open proposal.",
  silence:
    "The proposal is eligible once no decision has been recorded for this many seconds (0 turns this criterion off). It is also eligible while the epoch's client is frozen or expired.",
  "hold period":
    "The proposal is eligible once a hold has stood on IBCAdmin for this many seconds (0 turns this criterion off).",
  "proposal / opened at / executable at / cancel votes":
    "The open proposal's hash, when it opened (unix seconds; 0 means none), opened at + delay, and the guardian cancel votes so far.",
  "approvals by seat": "The proposal hash each guardian seat approved. All zeros means none.",
  "bridge / payout meter":
    "Capacity remaining for claim payouts. It refills toward the day cap at day cap / 86,400 per second; payouts consume it. It starts empty at launch and on a new payouts epoch.",
  "claims / sweeps":
    "Claims are received transfers owed to recipients, paid under the meters. Sweeps remain owed but require a governance decision before payment, for example when the credit is below the fee or above free escrow.",
  "claim states":
    "Open: unpaid or partly paid and queued. Withheld: unpaid remainder stays booked and can be restored. Forged: remainder is dropped and amounts already paid become losses. Done: paid in full.",
  "sends / outcomes":
    "A recorded send escrows native tokens or burns vouchers. Pending means no acknowledgement or timeout yet. Success means delivered; error or timeout owes the amount, excluding fees, back to the sender. Refunds may be paid immediately or queued.",
  "route state (gno)":
    "Closed routes accept no new credits, sends or payouts until governed reopening. Loss-closed routes take no new exposure while unresolved losses remain; voucher returns, acknowledgements, timeouts and refunds continue.",
  "route state (ethereum)":
    "Closed routes stop new receives and escrowed sends. Paused routes reject receives and escrowed sends while voucher burns continue. Refund flags hold refunds; an open recovery blocks receives.",
  "gno decision state":
    "Pending: recorded but not applied. Applied: executed. Cancelled: withdrawn before applying. Spent: recorded with no effect, such as a refusal against current state or an unpause during a hold.",
  "ethereum decision state":
    "Buffered: waiting for an earlier sequence or a working executor. Pending: recorded and waiting for delay or restrictions to clear. Executed: carried out. Spent: closed without effect. Cancelled: covered by a cancel decision.",
  check:
    "A monitoring rule evaluated against fresh snapshots. Each check reports its status, findings and evaluation time; its help icon explains the rule.",
  configuration:
    "Governed per-denom caps, fees and send bounds. Amounts use this denom’s display units.",
  "lock / supply":
    "Escrow accounting records native tokens locked for this route. Minted vouchers instead track outstanding, returning and proven-owed amounts against their supply cap. The accounting is separate from a physical token balance.",
  losses:
    "Recorded bridge losses, including forged payouts, refunds and receives. Recapitalization or liability reduction recovers them; accepting a deficit resolves it without recovering the backing.",
  recovery:
    "Guardian-proposed emergency route replacement that bypasses the governance channel. It needs the approval threshold, an eligible silent or held channel (or a frozen or expired client), and the configured delay.",
  "channel (gno)":
    "The governance epoch, client carrying its packets, and last Ethereum-bound decision sequence dispatched in that epoch.",
  "channel (ethereum)":
    "The governance epoch, router client carrying decisions from gno.land, and last decision sequence recorded in order.",
  "guardian / watch keys":
    "Seats 0–3 are guardians; seats 4–5 are monitor watch keys. Both can set flags. Two guardian seats can create a hold; watch keys never count toward it.",
  quarantine:
    "After a live validator-set transition, Ethereum token receives are rejected for this configured number of seconds. Zero disables quarantine. The deadline shows when the current quarantine ends.",
  "day cap / used level":
    "The Ethereum receive bucket’s cap and capacity used at the snapshot. Used capacity drains at day cap / 86,400 per second. Available capacity is cap minus used level. A zero cap rejects receives.",
  "refund cap / used level":
    "The Ethereum refund bucket’s cap and capacity used at the snapshot. Used capacity drains over time; available capacity is cap minus used level. A zero cap queues every refund.",
  "losses / accepted deficit":
    "Losses are amounts recorded by Ethereum settlements. Accepted deficit is the share accepted at reopening. Losses minus accepted deficit still awaits acceptance.",
  "suspect start / open":
    "Start is the index of the first acknowledgement or timeout processed at or after the suspect block. Open is the number of suspect records still unsettled.",
  "reconciliation per client":
    "A retired client’s open and suspect send records must be settled by governance before the route can reopen.",
  head: "The latest trusted header: a beacon slot for an Ethereum client on gno.land, or a gno.land block height for GnoLightClient. Its time and execution block, when available, are shown underneath.",
  epoch:
    "An epoch numbers governance decisions within one channel generation. A replacement or recovery advances it. The payout meter has a separate payouts epoch.",
  incidents:
    "A closure of the route: kind 1 is a replacement covering the client it retired; kind 2 is a late loss booked while the route was open.",
  "decision hash":
    "The keccak hash of the decision's envelope, which IBCAdmin uses to detect duplicates and conflicting payloads.",
  "sequence / timeout":
    "The packet's sequence on its source client, and its timeout (unix seconds).",
  "commitment ok":
    "The stored commitment is the commitment of the stored packet. A record failing it is not usable as evidence.",
  "sent at / processed at":
    "Unix seconds of the send and of the ack or timeout's processing, with the gno.land heights beside them.",
  "voucher supply (ethereum)":
    "GnoVoucher.totalSupply() of a gno-native denom's voucher (origin gno) at the pinned block. The gno.land lock of the denom backs it.",
  "supply (gno.land)":
    "The exact total supply of a minted denom's gno.land voucher. Ethereum's escrow balance of the token backs it.",
  origin:
    "Which chain a route token is native to: ethereum (an ERC20 escrowed on Ethereum, minted as a voucher on gno.land) or gno (the GnoVoucher of a gno-native denom).",
  "receive / refund available":
    "Cap minus level, floored at 0: what Ethereum can still receive or refund right now (at the pinned block). This is the same sense as a gno.land meter's level.",
  "day cap":
    "The bridge meter's ceiling and daily refill (day_cap). It is max(cap_floor, lock/10) for unescrow, recomputed at every refill, and cap_floor for mint.",
  outcome:
    "Pending: no acknowledgement or timeout has been processed. Success: delivered, nothing owed. Error or timeout: the amount, excluding the fee, is owed back to the sender.",
  payment:
    "Nothing owed means pending or successfully delivered. Paid means refunded immediately; queued means waiting. Claimed means a queued refund was paid later. Rejected means settled unpaid as delivered. Reconciled means paid by a settlement decision.",
  "backing by token":
    "For Ethereum-native tokens, Ethereum escrow backs the gno.land voucher supply. For gno-native tokens, gno.land escrow accounting backs the Ethereum voucher supply. Each balance is read at its chain’s pinned block.",
  "gno backing":
    "The gno.land voucher’s total supply for a minted denom, or the route’s escrow accounting for a native denom.",
  "ethereum backing":
    "The route escrow’s physical token balance for an Ethereum-native token, or GnoVoucher.totalSupply for a gno-native token.",
};

const aliases: Record<string, string> = {
  "incident (per client)": "incident",
  hash: "block hash / hash",
  "gno.land clients": "counts",
  "ethereum verifiers": "counts",
  routes: "counts",
  block: "height / block / slot",
  provenance: "provenance badge",
  freshness: "stale",
  "header verification": "header agreement",
  "value verification": "state verification / value verification",
  since: "first seen / since",
  verifier: "verifier version",
  "refund meter": "refund meter (gno.land)",
  "payout epoch": "meter epoch",
  "escrow accounting": "lock / escrow accounting",
  "queued refund amount": "queued refunds (amount)",
  "queued refunds": "queued refunds (count)",
  "waiting recipients": "waiting",
  "fees paid": "fees",
  "accepted losses": "accepted",
  "send minimum": "send max / send min (gno.land)",
  "send maximum": "send max / send min (gno.land)",
  "route loss totals": "unrecovered / unresolved (route)",
  "route key / client": "route key / client (ethereum)",
  "escrow contract": "escrow",
  holds: "route hold / refunds hold",
  flags: "flag (gno.land)",
  "token limits": "limits set",
  "token / units": "token",
  "max / message": "max per message",
  "send min / max": "send max / send min (ethereum)",
  watch: "watch key",
  hold: "hold (gno.land global)",
  executor: "executor / code hash",
  id: "decision",
  kind: "decision",
  text: "decision",
  when: "made / recorded / due / applied / cancelled",
  due: "made / recorded / due / applied / cancelled",
  seq: "seq (dispatch)",
  answer: "answer / status",
  recorded: "recorded / block",
  matures: "matures / mature at",
  head_age_s: "head / head_age_s / time_left_s / trusting_s",
  time_left_s: "head / head_age_s / time_left_s / trusting_s",
  trusting_s: "head / head_age_s / time_left_s / trusting_s",
  head_time: "head time",
  time_left: "expiry",
  gno_discovery: "gno discovery",
  current_client: "route key / client (ethereum)",
  gno_counterparty: "counterparty",
  gno_route: "route key",
  approvals: "threshold (approvals)",
  cancellations: "cancel threshold",
  proposal: "proposal / opened at / executable at / cancel votes",
  executable: "proposal / opened at / executable at / cancel votes",
  "cancel votes": "proposal / opened at / executable at / cancel votes",
  "last seq": "last seq (gno.land)",
  "auto-flag": "autoflag requested / effective",
  closed_pair: "planned checks",
  supply_identity: "planned checks",
  orphan_receives: "planned checks",
  proof_heights: "planned checks",
  header_record: "planned checks",
  foreign_decisions: "planned checks",
  "liveness_*": "planned checks",
  gov_ledger: "planned checks",
  "refund cap": "refund cap (gno.land)",
};

export function glossaryText(term: string): string | undefined {
  const key = term.toLowerCase().trim();
  return glossary[aliases[key] ?? key] ?? glossary[aliases[key.split("/")[0]] ?? key.split("/")[0]];
}
