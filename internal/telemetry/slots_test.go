package telemetry

import "testing"

func TestParseSlots_AllIdle(t *testing.T) {
	body := []byte(`[{"id":0,"is_processing":false},{"id":1,"is_processing":false}]`)
	s, err := ParseSlots(body)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalSlots != 2 || s.BusySlots != 0 {
		t.Errorf("got %+v, want total=2 busy=0", s)
	}
}

func TestParseSlots_Mixed(t *testing.T) {
	body := []byte(`[{"id":0,"is_processing":false},{"id":1,"is_processing":true},{"id":2,"is_processing":true}]`)
	s, err := ParseSlots(body)
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalSlots != 3 || s.BusySlots != 2 {
		t.Errorf("got %+v, want total=3 busy=2", s)
	}
}

func TestParseSlots_Empty(t *testing.T) {
	s, err := ParseSlots([]byte("[]"))
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalSlots != 0 || s.BusySlots != 0 {
		t.Errorf("got %+v, want zero", s)
	}
}

func TestParseSlots_BadJSON(t *testing.T) {
	if _, err := ParseSlots([]byte("not-json")); err == nil {
		t.Error("expected error for invalid JSON")
	}
}
