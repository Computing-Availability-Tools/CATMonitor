package statfs

import "testing"

func TestStatfsReal(t *testing.T) {
	// "/" always exists on Linux; just verify it returns non-zero total.
	s := Default()
	st, err := s.Statfs("/")
	if err != nil {
		t.Fatalf("Statfs(/) failed: %v", err)
	}
	if st.Total == 0 {
		t.Error("expected non-zero Total for /")
	}
	if st.Used != st.Total-st.Free {
		t.Errorf("Used should be Total-Free; got Total=%d Free=%d Used=%d", st.Total, st.Free, st.Used)
	}
}

func TestStatfsMockFetcher(t *testing.T) {
	ResetFetcher()
	defer ResetFetcher()
	SetFetcher(func(path string) (*Statfs, error) {
		if path == "/data" {
			return &Statfs{Total: 1000, Free: 200, Avail: 100, Used: 800}, nil
		}
		return nil, nil
	})
	st, err := Default().Statfs("/data")
	if err != nil {
		t.Fatalf("mock Statfs failed: %v", err)
	}
	if st.Total != 1000 || st.Used != 800 || st.Avail != 100 {
		t.Errorf("mock Statfs: got %+v", st)
	}
}

func TestAvailable(t *testing.T) {
	if !Default().Available() {
		t.Error("Available should be true on Linux")
	}
}
