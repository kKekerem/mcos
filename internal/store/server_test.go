package store

import (
	"testing"

	"mcos/internal/model"
)

func TestServerRoundTrip(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	srv := &model.Server{
		ID: "srv_test01", Name: "Test", Software: model.SoftwarePaper,
		MCVersion: "1.21.1", JavaMajor: 21, RAMMB: 2048, Port: 25565,
	}
	if err := st.SaveServer(srv); err != nil {
		t.Fatalf("SaveServer: %v", err)
	}
	if srv.CreatedAt.IsZero() || srv.UpdatedAt.IsZero() {
		t.Fatal("SaveServer should stamp CreatedAt/UpdatedAt")
	}

	got, err := st.GetServer("srv_test01")
	if err != nil {
		t.Fatalf("GetServer: %v", err)
	}
	if got.Name != "Test" || got.Software != model.SoftwarePaper || got.JavaMajor != 21 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	list, err := st.ListServers()
	if err != nil || len(list) != 1 {
		t.Fatalf("ListServers = %d (%v)", len(list), err)
	}

	if err := st.DeleteServer("srv_test01"); err != nil {
		t.Fatalf("DeleteServer: %v", err)
	}
	if _, err := st.GetServer("srv_test01"); err != ErrNotFound {
		t.Fatalf("GetServer after delete = %v, want ErrNotFound", err)
	}
}

func TestGetServerNotFound(t *testing.T) {
	st, _ := New(t.TempDir())
	if _, err := st.GetServer("nope"); err != ErrNotFound {
		t.Fatalf("GetServer(nope) = %v, want ErrNotFound", err)
	}
}
