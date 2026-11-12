package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fake(t *testing.T, content string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			w.Write([]byte(`{"models":[{"name":"llama3.2:3b"},{"name":"gpt-oss:120b-cloud","remote_host":"https://ollama.com"}]}`))
		case "/api/chat":
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]string{"content": content}})
		}
	}))
}

func TestEndpointAndCloudModelsRefused(t *testing.T) {
	for _, u := range []string{"https://ollama.com", "http://10.0.0.2:11434", "http://example.com:11434"} {
		if _, err := ValidateEndpoint(u); err == nil {
			t.Errorf("%s accepted", u)
		}
	}
	srv := fake(t, `{}`)
	defer srv.Close()
	st := Detect(context.Background(), srv.URL)
	if !st.Available || len(st.Models) != 2 || st.Models[1].Local {
		t.Fatalf("%+v", st)
	}
	if _, err := Suggest(context.Background(), srv.URL, "gpt-oss:120b-cloud", "x"); err == nil {
		t.Error("cloud model used")
	}
	if st := Detect(context.Background(), "http://127.0.0.1:1"); st.Available {
		t.Error("unreachable reported available")
	}
}

func TestSuggestionValidated(t *testing.T) {
	src := "Tidewell builds cold-chain monitoring sensors and software for seafood exporters in Bangladesh."
	srv := fake(t, `{"sector":"Logistics & mobility","summary":"Tidewell builds cold-chain monitoring sensors for seafood exporters."}`)
	defer srv.Close()
	s, err := Suggest(context.Background(), srv.URL, "llama3.2:3b", src)
	if err != nil || s.Summary == "" || s.Sector == "" {
		t.Fatalf("%+v %v", s, err)
	}
	bad := fake(t, `{"sector":"Approved","summary":"Raised $50 million from famous investors and is profitable worldwide."}`)
	defer bad.Close()
	s, _ = Suggest(context.Background(), bad.URL, "llama3.2:3b", src)
	if s.Summary != "" || s.Sector != "" || len(s.Rejected) != 2 {
		t.Fatalf("ungrounded output accepted: %+v", s)
	}
}
