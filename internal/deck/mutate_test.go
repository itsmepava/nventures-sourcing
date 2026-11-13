package deck

import (
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestMutationHunt mutates the real decks for 60 s with panic recovery off
// and a 3 s watchdog per input. Opt-in: DECK_MUTATION_HUNT=1 go test -run MutationHunt ./internal/deck
func TestMutationHunt(t *testing.T) {
	if os.Getenv("DECK_MUTATION_HUNT") == "" {
		t.Skip("set DECK_MUTATION_HUNT=1")
	}
	seeds := [][]byte{read(t, "lumenfold.pdf"), read(t, "reportlab.pdf"), read(t, "chromium.pdf"), read(t, "lumenfold.pptx")}
	toks := [][]byte{[]byte("<<"), []byte(">>"), []byte("["), []byte("]"), []byte("("), []byte(")"), []byte(" obj "), []byte("stream\n"), []byte("endstream"), []byte(" 0 R "), []byte("BI "), []byte(" EI "), []byte("/Kids"), []byte("Do "), []byte("Tf "), []byte("<00"), []byte("%")}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	deadline := time.Now().Add(60 * time.Second)
	recoverPanics = false
	defer func() { recoverPanics = true }()
	n := 0
	for time.Now().Before(deadline) {
		b := append([]byte(nil), seeds[rng.Intn(len(seeds))]...)
		for k := rng.Intn(8) + 1; k > 0; k-- {
			p := rng.Intn(len(b))
			switch rng.Intn(4) {
			case 0:
				b[p] = byte(rng.Intn(256))
			case 1:
				b = b[:p]
			case 2:
				tk := toks[rng.Intn(len(toks))]
				b = append(b[:p], append(append([]byte(nil), tk...), b[p:]...)...)
			case 3:
				q := p + rng.Intn(len(b)-p)
				b = append(b[:p], b[q:]...)
			}
			if len(b) < 2 {
				break
			}
		}
		done := make(chan struct{})
		panicked := make(chan any, 1)
		go func() {
			defer func() {
				if p := recover(); p != nil {
					panicked <- p
				}
				close(done)
			}()
			Extract("x.pdf", "", b)
		}()
		select {
		case <-done:
			select {
			case p := <-panicked:
				os.WriteFile(filepath.Join(os.TempDir(), "deck-panic.bin"), b, 0o644)
				t.Fatalf("panic after %d runs: %v", n, p)
			default:
			}
		case <-time.After(3 * time.Second):
			os.WriteFile(filepath.Join(os.TempDir(), "deck-hang.bin"), b, 0o644)
			t.Fatalf("hang after %d runs; input saved (%d bytes)", n, len(b))
		}
		n++
	}
	t.Logf("%d runs, no hang", n)
}
