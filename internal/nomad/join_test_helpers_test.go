package nomad

import (
	"database/sql"
	"encoding/json"
	"net"
	"sync"
	"testing"

	"panel/internal/linux"
	"panel/internal/server"
)

var joinTestDBByServer sync.Map

func registerJoinTestDB(svc *server.Service, db *sql.DB) func() {
	joinTestDBByServer.Store(svc, db)
	return func() {
		joinTestDBByServer.Delete(svc)
	}
}

func markJoinTestServerEligible(t *testing.T, svc *server.Service, serverID string) {
	t.Helper()
	setJoinTestServerState(t, svc, serverID, "debian", "12", "Debian GNU/Linux 12", true, true, true)
}

func setJoinTestServerState(t *testing.T, svc *server.Service, serverID, osID, osVersionID, osPrettyName string, supported, reachable, passwordlessSudo bool) {
	t.Helper()
	dbAny, ok := joinTestDBByServer.Load(svc)
	if !ok {
		t.Fatalf("test database was not registered for server service")
	}
	db := dbAny.(*sql.DB)
	_, err := db.Exec(
		`UPDATE servers SET os_id=?,os_version_id=?,os_pretty_name=?,os_supported=?,reachable=?,sudo_passwordless=? WHERE id=?`,
		osID,
		osVersionID,
		osPrettyName,
		testBoolInt(supported),
		testBoolInt(reachable),
		testBoolInt(passwordlessSudo),
		serverID,
	)
	if err != nil {
		t.Fatal(err)
	}
	var host, rawTraits string
	if err := db.QueryRow(`SELECT host,traits FROM servers WHERE id=?`, serverID).Scan(&host, &rawTraits); err != nil {
		t.Fatal(err)
	}
	traits := map[string]string{}
	_ = json.Unmarshal([]byte(rawTraits), &traits)
	if ip := net.ParseIP(host); ip != nil {
		suffix := "/24"
		family := "inet"
		if ip.To4() == nil {
			suffix = "/64"
			family = "inet6"
		}
		traits["sys.network_interfaces"] = "eth0|" + family + "|" + ip.String() + suffix
		traits[TraitAdvertiseAddress] = ip.String()
		traits[TraitServerAdvertiseAddress] = ip.String()
	}
	traitsJSON, _ := json.Marshal(traits)
	if _, err := db.Exec(`UPDATE servers SET traits=? WHERE id=?`, string(traitsJSON), serverID); err != nil {
		t.Fatal(err)
	}
}

func mustNomadAdapter(t *testing.T, svc *JoinService, srv server.Server) linux.DistroAdapter {
	t.Helper()
	adapter, err := svc.ensureNomadEligible(srv)
	if err != nil {
		t.Fatal(err)
	}
	return adapter
}

func testBoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
