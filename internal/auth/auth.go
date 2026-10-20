// Package auth identifies players by API key.
//
// A key is generated once at registration and shown to the player exactly
// once; only its SHA-256 hash is stored. Every gRPC call except a small public
// allow-list must carry the key in the `authorization` metadata header
// (`Bearer <key>`) or the `x-api-key` header.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/Atul-Koundal/protoverse/internal/domain"
)

const keyPrefix = "pv_"

// NewKey generates a fresh random API key and returns it together with the
// hash that should be stored.
func NewKey() (raw, hash string, err error) {
	b := make([]byte, 24)
	if _, err = rand.Read(b); err != nil {
		return "", "", err
	}
	raw = keyPrefix + hex.EncodeToString(b)
	return raw, HashKey(raw), nil
}

// HashKey returns the hex SHA-256 of an API key. API keys are long random
// strings (not user-chosen passwords), so a fast unsalted hash is appropriate
// and lets us look a player up by hash with a plain index.
func HashKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// Lookup finds a player by API-key hash. Implemented by *repository.Repository.
type Lookup interface {
	GetPlayerByAPIKeyHash(ctx context.Context, hash string) (*domain.Player, error)
}

type ctxKey struct{}

// WithPlayer returns a context carrying the authenticated player.
func WithPlayer(ctx context.Context, p *domain.Player) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PlayerFromContext returns the authenticated player, if any.
func PlayerFromContext(ctx context.Context) (*domain.Player, bool) {
	p, ok := ctx.Value(ctxKey{}).(*domain.Player)
	return p, ok && p != nil
}

// RequirePlayer is what handlers call to get the caller, or an
// Unauthenticated error if the interceptor didn't attach one.
func RequirePlayer(ctx context.Context) (*domain.Player, error) {
	p, ok := PlayerFromContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "authentication required")
	}
	return p, nil
}

// ExtractKey pulls the API key out of incoming gRPC metadata.
func ExtractKey(md metadata.MD) string {
	if v := md.Get("authorization"); len(v) > 0 {
		h := strings.TrimSpace(v[0])
		if len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
			return strings.TrimSpace(h[7:])
		}
		return h
	}
	if v := md.Get("x-api-key"); len(v) > 0 {
		return strings.TrimSpace(v[0])
	}
	return ""
}

// Public is the set of full gRPC method names callable without a key.
type Public map[string]bool

// DefaultPublic lists the methods anyone may call: sign-up, read-only world
// info, and gRPC server reflection (so grpcurl keeps working).
func DefaultPublic() Public {
	const svc = "/protoverse.v1.GameService/"
	return Public{
		svc + "CreateAccount":  true,
		svc + "GetCatalog":     true,
		svc + "GetGalaxyState": true,
		svc + "GetLeaderboard": true,
	}
}

func (p Public) allows(method string) bool {
	return p[method] || strings.HasPrefix(method, "/grpc.reflection.")
}

func authenticate(ctx context.Context, l Lookup) (*domain.Player, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	key := ExtractKey(md)
	if key == "" {
		return nil, status.Error(codes.Unauthenticated, "missing API key (send `authorization: Bearer <key>`)")
	}
	hash := HashKey(key)
	player, err := l.GetPlayerByAPIKeyHash(ctx, hash)
	if err != nil {
		return nil, status.Error(codes.Internal, "failed to verify API key")
	}
	// Constant-time compare as a belt-and-braces guard; the DB lookup already matched.
	if player == nil || subtle.ConstantTimeCompare([]byte(player.APIKey), []byte(hash)) != 1 {
		return nil, status.Error(codes.Unauthenticated, "invalid API key")
	}
	return player, nil
}

// UnaryInterceptor authenticates unary calls.
func UnaryInterceptor(l Lookup, public Public) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if public.allows(info.FullMethod) {
			return handler(ctx, req)
		}
		player, err := authenticate(ctx, l)
		if err != nil {
			return nil, err
		}
		return handler(WithPlayer(ctx, player), req)
	}
}

// StreamInterceptor authenticates streaming calls.
func StreamInterceptor(l Lookup, public Public) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
		if public.allows(info.FullMethod) {
			return handler(srv, ss)
		}
		player, err := authenticate(ss.Context(), l)
		if err != nil {
			return err
		}
		return handler(srv, &authedStream{ServerStream: ss, ctx: WithPlayer(ss.Context(), player)})
	}
}

// authedStream overrides Context() so stream handlers can see the player.
type authedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *authedStream) Context() context.Context { return s.ctx }
