package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/hollis-labs/surface/module"
)

func TestTheGatewayGoesOnlyToAModuleThatDeclaresIt(t *testing.T) {
	a := &App{}
	a.SetGateway(func(context.Context) (module.Gateway, error) {
		return module.Gateway{Model: "m", URL: "http://gw", KeyFile: "/k"}, nil
	})
	if _, err := (moduleAgent{app: a, id: "quiet"}).Gateway(t.Context()); !errors.Is(err, module.ErrNoAgent) {
		t.Fatalf("an undeclared module got the gateway: %v", err)
	}
	gw, err := (moduleAgent{app: a, id: "workbench", declared: true}).Gateway(t.Context())
	if err != nil || gw.URL != "http://gw" {
		t.Fatal(gw, err)
	}
	if b, _ := json.Marshal(gw); strings.Contains(string(b), "/k") {
		t.Fatalf("the key file's path is marshaled: %s", b)
	}
	if _, err := (moduleAgent{app: &App{}, id: "workbench", declared: true}).Gateway(t.Context()); !errors.Is(err, module.ErrNoAgent) {
		t.Fatalf("no source: %v", err)
	}
}
