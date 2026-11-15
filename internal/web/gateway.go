// Package web is a thin HTTP/JSON + Server-Sent-Events gateway in front of the
// gRPC API, plus the embedded browser client. Browsers can't speak gRPC
// directly, so the gateway translates: it holds no game logic and no state,
// it just forwards each request (with the player's API key) to the API server.
package web

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
)

//go:embed static
var staticFiles embed.FS

const maxBodyBytes = 1 << 20

var (
	marshaler   = protojson.MarshalOptions{EmitUnpopulated: true}
	unmarshaler = protojson.UnmarshalOptions{DiscardUnknown: true}
)

// Gateway serves the web client and the JSON API.
type Gateway struct {
	client protoversev1.GameServiceClient
}

func New(client protoversev1.GameServiceClient) *Gateway {
	return &Gateway{client: client}
}

// Handler returns the full HTTP handler (API under /api/, client at /).
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/catalog", func(w http.ResponseWriter, r *http.Request) {
		g.unary(w, r, &protoversev1.GetCatalogRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.GetCatalog(ctx, req.(*protoversev1.GetCatalogRequest))
		})
	})
	mux.HandleFunc("GET /api/galaxy", func(w http.ResponseWriter, r *http.Request) {
		g.unary(w, r, &protoversev1.GetGalaxyStateRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.GetGalaxyState(ctx, req.(*protoversev1.GetGalaxyStateRequest))
		})
	})
	mux.HandleFunc("GET /api/me", func(w http.ResponseWriter, r *http.Request) {
		g.unary(w, r, &protoversev1.GetMyStateRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.GetMyState(ctx, req.(*protoversev1.GetMyStateRequest))
		})
	})
	mux.HandleFunc("GET /api/leaderboard", func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		g.unary(w, r, &protoversev1.GetLeaderboardRequest{Limit: int32(limit)}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.GetLeaderboard(ctx, req.(*protoversev1.GetLeaderboardRequest))
		})
	})

	mux.HandleFunc("POST /api/register", func(w http.ResponseWriter, r *http.Request) {
		g.unary(w, r, &protoversev1.CreateAccountRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.CreateAccount(ctx, req.(*protoversev1.CreateAccountRequest))
		})
	})
	mux.HandleFunc("POST /api/move", func(w http.ResponseWriter, r *http.Request) {
		g.unary(w, r, &protoversev1.MoveFleetRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.MoveFleet(ctx, req.(*protoversev1.MoveFleetRequest))
		})
	})
	mux.HandleFunc("POST /api/attack", func(w http.ResponseWriter, r *http.Request) {
		g.unary(w, r, &protoversev1.AttackRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.Attack(ctx, req.(*protoversev1.AttackRequest))
		})
	})
	mux.HandleFunc("POST /api/build", func(w http.ResponseWriter, r *http.Request) {
		g.unary(w, r, &protoversev1.BuildShipRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.BuildShip(ctx, req.(*protoversev1.BuildShipRequest))
		})
	})
	mux.HandleFunc("POST /api/colonize", func(w http.ResponseWriter, r *http.Request) {
		g.unary(w, r, &protoversev1.ColonizeRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.Colonize(ctx, req.(*protoversev1.ColonizeRequest))
		})
	})
	mux.HandleFunc("POST /api/invade", func(w http.ResponseWriter, r *http.Request) {
		g.unary(w, r, &protoversev1.InvadePlanetRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.InvadePlanet(ctx, req.(*protoversev1.InvadePlanetRequest))
		})
	})
	mux.HandleFunc("POST /api/fortify", func(w http.ResponseWriter, r *http.Request) {
		g.unary(w, r, &protoversev1.FortifyPlanetRequest{}, func(ctx context.Context, req proto.Message) (proto.Message, error) {
			return g.client.FortifyPlanet(ctx, req.(*protoversev1.FortifyPlanetRequest))
		})
	})
	mux.HandleFunc("GET /api/stream", g.stream)

	sub, err := fs.Sub(staticFiles, "static")
	if err != nil {
		panic(err) // the embed directive guarantees this exists
	}
	mux.Handle("/", http.FileServerFS(sub))

	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

// outgoing attaches the caller's API key (header or ?key=) to the gRPC call.
func outgoing(r *http.Request) context.Context {
	key := ""
	if h := r.Header.Get("Authorization"); h != "" {
		key = strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
	} else if k := r.URL.Query().Get("key"); k != "" {
		key = k // EventSource can't set headers, so the stream passes it in the URL
	}
	ctx := r.Context()
	if key != "" {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+key)
	}
	return ctx
}

// unary decodes the JSON body into req (for POSTs), runs the gRPC call and
// writes the JSON response or a mapped error.
func (g *Gateway) unary(w http.ResponseWriter, r *http.Request, req proto.Message, call func(context.Context, proto.Message) (proto.Message, error)) {
	if r.Method == http.MethodPost {
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
		if err != nil {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		if len(body) > 0 {
			if err := unmarshaler.Unmarshal(body, req); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
				return
			}
		}
	}
	resp, err := call(outgoing(r), req)
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	out, err := marshaler.Marshal(resp)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode response")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(out)
}

// stream relays the player's gRPC event stream as Server-Sent Events.
func (g *Gateway) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	ctx, cancel := context.WithCancel(outgoing(r))
	defer cancel()

	stream, err := g.client.StreamGameState(ctx, &protoversev1.StreamGameStateRequest{})
	if err != nil {
		writeGRPCError(w, err)
		return
	}
	// The first Recv surfaces auth errors; read it before committing to SSE headers.
	first, err := stream.Recv()
	if err != nil {
		if status.Code(err) == codes.Unauthenticated {
			writeGRPCError(w, err)
		}
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	fmt.Fprint(w, ": connected\n\n")
	flusher.Flush()

	send := func(ev *protoversev1.GameEvent) bool {
		b, err := marshaler.Marshal(ev)
		if err != nil {
			return true
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send(first) {
		return
	}
	for {
		ev, err := stream.Recv()
		if err != nil || !send(ev) {
			return
		}
	}
}

func httpStatus(c codes.Code) int {
	switch c {
	case codes.InvalidArgument:
		return http.StatusBadRequest
	case codes.Unauthenticated:
		return http.StatusUnauthorized
	case codes.PermissionDenied:
		return http.StatusForbidden
	case codes.NotFound:
		return http.StatusNotFound
	case codes.AlreadyExists, codes.FailedPrecondition:
		return http.StatusConflict
	case codes.ResourceExhausted:
		return http.StatusTooManyRequests
	case codes.Unavailable:
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}

func writeGRPCError(w http.ResponseWriter, err error) {
	st, _ := status.FromError(err)
	msg := st.Message()
	if st.Code() == codes.Internal || st.Code() == codes.Unknown {
		msg = "internal error" // don't leak server internals to browsers
	}
	writeError(w, httpStatus(st.Code()), msg)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
