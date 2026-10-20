package auth

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

func TestNewKeyAndHash(t *testing.T) {
	raw, hash, err := NewKey()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "pv_") || len(raw) < 40 {
		t.Errorf("unexpected key format %q", raw)
	}
	if hash != HashKey(raw) || hash == raw {
		t.Error("hash should be the deterministic SHA-256 of the key, not the key itself")
	}
	raw2, _, _ := NewKey()
	if raw == raw2 {
		t.Error("keys must be unique")
	}
}

func TestExtractKey(t *testing.T) {
	tests := []struct {
		name string
		md   metadata.MD
		want string
	}{
		{"bearer", metadata.Pairs("authorization", "Bearer pv_abc"), "pv_abc"},
		{"bearer case-insensitive", metadata.Pairs("authorization", "bearer pv_abc"), "pv_abc"},
		{"raw authorization", metadata.Pairs("authorization", "pv_abc"), "pv_abc"},
		{"x-api-key", metadata.Pairs("x-api-key", "pv_abc"), "pv_abc"},
		{"none", metadata.MD{}, ""},
	}
	for _, tt := range tests {
		if got := ExtractKey(tt.md); got != tt.want {
			t.Errorf("%s: got %q want %q", tt.name, got, tt.want)
		}
	}
}

type fakeLookup struct{ players map[string]*domain.Player }

func (f fakeLookup) GetPlayerByAPIKeyHash(_ context.Context, hash string) (*domain.Player, error) {
	return f.players[hash], nil
}

func TestUnaryInterceptor(t *testing.T) {
	raw, hash, _ := NewKey()
	alice := &domain.Player{DisplayName: "alice", APIKey: hash}
	lookup := fakeLookup{players: map[string]*domain.Player{hash: alice}}
	intercept := UnaryInterceptor(lookup, DefaultPublic())

	var seen *domain.Player
	handler := func(ctx context.Context, _ any) (any, error) {
		seen, _ = PlayerFromContext(ctx)
		return "ok", nil
	}
	call := func(method, key string) error {
		ctx := context.Background()
		if key != "" {
			ctx = metadata.NewIncomingContext(ctx, metadata.Pairs("authorization", "Bearer "+key))
		}
		seen = nil
		_, err := intercept(ctx, nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
		return err
	}

	if err := call("/protoverse.v1.GameService/MoveFleet", raw); err != nil || seen != alice {
		t.Errorf("valid key should authenticate: err=%v seen=%v", err, seen)
	}
	if err := call("/protoverse.v1.GameService/MoveFleet", ""); status.Code(err) != codes.Unauthenticated {
		t.Errorf("missing key: got %v", err)
	}
	if err := call("/protoverse.v1.GameService/MoveFleet", "pv_wrong"); status.Code(err) != codes.Unauthenticated {
		t.Errorf("wrong key: got %v", err)
	}
	if err := call("/protoverse.v1.GameService/CreateAccount", ""); err != nil {
		t.Errorf("public method should not need a key: %v", err)
	}
	if err := call("/grpc.reflection.v1.ServerReflection/ServerReflectionInfo", ""); err != nil {
		t.Errorf("reflection should be public: %v", err)
	}
	if _, err := RequirePlayer(context.Background()); status.Code(err) != codes.Unauthenticated {
		t.Errorf("RequirePlayer without a player: got %v", err)
	}
}
