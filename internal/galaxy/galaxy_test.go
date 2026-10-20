package galaxy

import (
	"math/rand"
	"testing"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

func TestGenerate(t *testing.T) {
	planets := Generate(42, 40)
	if len(planets) != 40 {
		t.Fatalf("got %d planets, want 40", len(planets))
	}
	names := map[string]bool{}
	for _, p := range planets {
		if !domain.InBounds(p.PosX, p.PosY) {
			t.Errorf("%s out of bounds: (%v, %v)", p.Name, p.PosX, p.PosY)
		}
		if p.Production < 5 || p.Production > 25 {
			t.Errorf("%s production %d out of range", p.Name, p.Production)
		}
		if p.Resources < 2000 || p.Resources > 10000 {
			t.Errorf("%s resources %d out of range", p.Name, p.Resources)
		}
		if names[p.Name] {
			t.Errorf("duplicate planet name %q", p.Name)
		}
		names[p.Name] = true
		if p.OwnerID != nil {
			t.Errorf("%s should start neutral", p.Name)
		}
	}
}

func TestGenerateDeterministic(t *testing.T) {
	a, b := Generate(7, 20), Generate(7, 20)
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("planet %d differs between runs with the same seed", i)
		}
	}
	c := Generate(8, 20)
	same := true
	for i := range a {
		if a[i] != c[i] {
			same = false
		}
	}
	if same {
		t.Error("different seeds produced identical galaxies")
	}
}

func TestPickHomeKeepsDistance(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	existing := Generate(3, 30)
	for i := 0; i < 50; i++ {
		x, y := PickHome(rng, existing)
		if !domain.InBounds(x, y) {
			t.Fatalf("home (%v, %v) out of bounds", x, y)
		}
		for _, p := range existing {
			if d := domain.Distance(x, y, p.PosX, p.PosY); d < domain.MinHomeSpacing {
				t.Fatalf("home (%v,%v) only %.1f from %s", x, y, d, p.Name)
			}
		}
	}
}
