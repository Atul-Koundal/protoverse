package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	protoversev1 "github.com/Atul-Koundal/protoverse/gen/protoverse/v1"
)

// fakeClient implements just the RPCs the tests exercise; embedding the
// interface makes any other call panic, which would flag an unexpected route.
type fakeClient struct {
	protoversev1.GameServiceClient
	lastAuth    string
	lastMove    *protoversev1.MoveFleetRequest
	lastBuild   *protoversev1.BuildShipRequest
	lastInvade  *protoversev1.InvadePlanetRequest
	lastFortify *protoversev1.FortifyPlanetRequest
}

func (f *fakeClient) record(ctx context.Context) {
	md, _ := metadata.FromOutgoingContext(ctx)
	if v := md.Get("authorization"); len(v) > 0 {
		f.lastAuth = v[0]
	} else {
		f.lastAuth = ""
	}
}

func (f *fakeClient) CreateAccount(_ context.Context, req *protoversev1.CreateAccountRequest, _ ...grpc.CallOption) (*protoversev1.Player, error) {
	if req.GetDisplayName() == "taken" {
		return nil, status.Error(codes.AlreadyExists, "display name \"taken\" is already taken")
	}
	return &protoversev1.Player{Id: "p1", DisplayName: req.GetDisplayName(), ApiKey: "pv_secret"}, nil
}

func (f *fakeClient) GetMyState(ctx context.Context, _ *protoversev1.GetMyStateRequest, _ ...grpc.CallOption) (*protoversev1.PlayerState, error) {
	f.record(ctx)
	if f.lastAuth == "" {
		return nil, status.Error(codes.Unauthenticated, "missing API key")
	}
	return &protoversev1.PlayerState{Credits: 500}, nil
}

func (f *fakeClient) MoveFleet(ctx context.Context, req *protoversev1.MoveFleetRequest, _ ...grpc.CallOption) (*protoversev1.ActionAck, error) {
	f.record(ctx)
	f.lastMove = req
	return &protoversev1.ActionAck{ActionId: "a1"}, nil
}

func (f *fakeClient) BuildShip(ctx context.Context, req *protoversev1.BuildShipRequest, _ ...grpc.CallOption) (*protoversev1.ActionAck, error) {
	f.record(ctx)
	f.lastBuild = req
	return nil, status.Error(codes.Internal, "pq: secret database detail")
}

func (f *fakeClient) InvadePlanet(ctx context.Context, req *protoversev1.InvadePlanetRequest, _ ...grpc.CallOption) (*protoversev1.ActionAck, error) {
	f.record(ctx)
	f.lastInvade = req
	return &protoversev1.ActionAck{ActionId: "inv1"}, nil
}

func (f *fakeClient) FortifyPlanet(ctx context.Context, req *protoversev1.FortifyPlanetRequest, _ ...grpc.CallOption) (*protoversev1.ActionAck, error) {
	f.record(ctx)
	f.lastFortify = req
	return nil, status.Error(codes.FailedPrecondition, "not enough credits")
}

func serve(t *testing.T, c protoversev1.GameServiceClient) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(New(c).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func do(t *testing.T, method, url, body, auth string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(method, url, strings.NewReader(body))
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var sb strings.Builder
	buf := make([]byte, 4096)
	for {
		n, err := resp.Body.Read(buf)
		sb.Write(buf[:n])
		if err != nil {
			break
		}
	}
	return resp, sb.String()
}

func TestServesClient(t *testing.T) {
	srv := serve(t, &fakeClient{})
	resp, body := do(t, "GET", srv.URL+"/", "", "")
	if resp.StatusCode != 200 || !strings.Contains(body, "PROTOVERSE") {
		t.Fatalf("index not served: %d", resp.StatusCode)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("X-Frame-Options") != "DENY" {
		t.Error("security headers missing")
	}
}

func TestRegisterPassthroughAndErrorMapping(t *testing.T) {
	srv := serve(t, &fakeClient{})

	resp, body := do(t, "POST", srv.URL+"/api/register", `{"displayName":"alice"}`, "")
	if resp.StatusCode != 200 || !strings.Contains(body, `"apiKey":"pv_secret"`) {
		t.Errorf("register: %d %s", resp.StatusCode, body)
	}
	if resp, _ := do(t, "POST", srv.URL+"/api/register", `{"displayName":"taken"}`, ""); resp.StatusCode != http.StatusConflict {
		t.Errorf("AlreadyExists should map to 409, got %d", resp.StatusCode)
	}
	if resp, _ := do(t, "POST", srv.URL+"/api/register", `{not json`, ""); resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad JSON should be 400, got %d", resp.StatusCode)
	}
}

func TestAuthForwardedAndRejected(t *testing.T) {
	fc := &fakeClient{}
	srv := serve(t, fc)

	if resp, _ := do(t, "GET", srv.URL+"/api/me", "", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("missing key should be 401, got %d", resp.StatusCode)
	}
	if resp, body := do(t, "GET", srv.URL+"/api/me", "", "Bearer pv_abc"); resp.StatusCode != 200 || !strings.Contains(body, `"500"`) {
		t.Errorf("authed call: %d %s", resp.StatusCode, body) // int64 credits are JSON strings
	}
	if fc.lastAuth != "Bearer pv_abc" {
		t.Errorf("key not forwarded to gRPC: %q", fc.lastAuth)
	}
	if resp, _ := do(t, "GET", srv.URL+"/api/me?key=pv_query", "", ""); resp.StatusCode != 200 || fc.lastAuth != "Bearer pv_query" {
		t.Errorf("?key= fallback failed: %d %q", resp.StatusCode, fc.lastAuth)
	}
}

func TestOrdersDecodeJSONBodies(t *testing.T) {
	fc := &fakeClient{}
	srv := serve(t, fc)

	do(t, "POST", srv.URL+"/api/move", `{"fleetId":"f1","destination":{"x":12.5,"y":0}}`, "Bearer k")
	if fc.lastMove.GetFleetId() != "f1" || fc.lastMove.GetDestination().GetX() != 12.5 {
		t.Errorf("move body not decoded: %v", fc.lastMove)
	}
	// Server-side internals must never leak to the browser.
	resp, body := do(t, "POST", srv.URL+"/api/build", `{"planetId":"p","shipType":"scout","quantity":2}`, "Bearer k")
	if resp.StatusCode != 500 || strings.Contains(body, "secret") || !strings.Contains(body, "internal error") {
		t.Errorf("internal errors must be masked: %d %s", resp.StatusCode, body)
	}
	if fc.lastBuild.GetQuantity() != 2 {
		t.Error("build body not decoded")
	}
	if resp, _ := do(t, "GET", srv.URL+"/api/move", "", ""); resp.StatusCode == 200 {
		t.Error("GET on a POST-only route must not succeed")
	}
}

func TestPlanetOrdersRoutes(t *testing.T) {
	fc := &fakeClient{}
	srv := serve(t, fc)

	resp, body := do(t, "POST", srv.URL+"/api/invade", `{"fleetId":"f1","planetId":"p9"}`, "Bearer k")
	if resp.StatusCode != 200 || !strings.Contains(body, "inv1") || fc.lastInvade.GetFleetId() != "f1" || fc.lastInvade.GetPlanetId() != "p9" {
		t.Errorf("invade: %d %s %v", resp.StatusCode, body, fc.lastInvade)
	}
	resp, _ = do(t, "POST", srv.URL+"/api/fortify", `{"planetId":"p9"}`, "Bearer k")
	if resp.StatusCode != http.StatusConflict || fc.lastFortify.GetPlanetId() != "p9" {
		t.Errorf("fortify: FailedPrecondition should map to 409, got %d", resp.StatusCode)
	}
}
