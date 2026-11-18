package core

import (
	"encoding/json"
	"fmt"
	"testing"
)

// Generated UI on the real kernel and Substrate's gateway: a surface is shown as a card, the
// owner's action is journaled and executed once, and a high-risk choice needs a second step.
func TestGenuiSurfacesAreShownAnsweredOnceAndConfirmedWhenRisky(t *testing.T) {
	srv := boot(t, "scratch")

	type shown struct {
		Card     string `json:"card"`
		Envelope struct {
			ID   string `json:"id"`
			Data struct {
				CreateSurface struct {
					Components []struct {
						Component string `json:"component"`
						Props     struct {
							Choices []struct {
								Label        string          `json:"label"`
								CallFunction json.RawMessage `json:"callFunction"`
							} `json:"choices"`
							Submit struct {
								CallFunction json.RawMessage `json:"callFunction"`
							} `json:"submit"`
						} `json:"props"`
					} `json:"components"`
				} `json:"createSurface"`
			} `json:"data"`
		} `json:"envelope"`
	}
	type response struct {
		AgentFunctionResponse struct {
			Value struct {
				Status   string          `json:"status"`
				Result   json.RawMessage `json:"result"`
				Recorded *struct {
					Status string `json:"status"`
				} `json:"recorded"`
			} `json:"value"`
		} `json:"agentFunctionResponse"`
	}
	n := 0
	call := func(surface string, fn json.RawMessage) string {
		n++
		return fmt.Sprintf(`{"version":"1.0","callAgentFunction":{"surfaceId":%q,"functionCallId":"call-%d","callFunction":%s}}`, surface, n, fn)
	}
	card := func(id string) (c struct {
		Status  string `json:"status"`
		Outcome struct {
			Data map[string]any `json:"data"`
		} `json:"outcome"`
	}) {
		t.Helper()
		if code := do(t, srv, "GET", "/api/cards/"+id, "", &c); code != 200 {
			t.Fatalf("card %s: %d", id, code)
		}
		return c
	}

	// The agent cannot send components or unknown parameters: only a pattern's own.
	if code := do(t, srv, "POST", "/api/genui/surfaces", `{"pattern":"approval","params":{"title":"x","choices":[{"id":"a","label":"A"}],"html":"<b>hi</b>"}}`, nil); code == 200 {
		t.Error("an approval with a parameter the pattern does not have was shown")
	}
	if code := do(t, srv, "POST", "/api/genui/surfaces", `{"pattern":"dashboard","params":{}}`, nil); code == 200 {
		t.Error("a pattern that is not one of the three was shown")
	}

	// An approval: shown as an open card, answered through the gateway, closed with the choice.
	var a shown
	if code := do(t, srv, "POST", "/api/genui/surfaces", `{"pattern":"approval","params":{"title":"Publish the draft?","summary":"It goes to the site.","changes":[{"label":"Status","before":"draft","after":"published"}],"choices":[{"id":"publish","label":"Publish"},{"id":"hold","label":"Not yet"}]}}`, &a); code != 200 {
		t.Fatalf("show approval: %d", code)
	}
	choices := a.Envelope.Data.CreateSurface.Components[0].Props.Choices
	if a.Card == "" || len(choices) != 2 || card(a.Card).Status != "" {
		t.Fatalf("shown: %+v", a)
	}
	var res response
	if code := do(t, srv, "POST", "/api/genui/actions", call(a.Envelope.ID, choices[0].CallFunction), &res); code != 200 || res.AgentFunctionResponse.Value.Status != "ok" {
		t.Fatalf("act: %d %+v", code, res)
	}
	if c := card(a.Card); c.Status != "submitted" || c.Outcome.Data["choice"] != "publish" {
		t.Errorf("the card after the action: %+v", c)
	}
	// The other choice shares the action: taking it now conflicts and nothing runs again.
	res = response{}
	do(t, srv, "POST", "/api/genui/actions", call(a.Envelope.ID, choices[1].CallFunction), &res)
	if v := res.AgentFunctionResponse.Value; v.Status != "conflict" || v.Recorded == nil || v.Recorded.Status != "ok" {
		t.Errorf("the second choice: %+v, want a conflict carrying the recorded ok", v)
	}
	if c := card(a.Card); c.Outcome.Data["choice"] != "publish" {
		t.Errorf("the card changed after a conflicting action: %+v", c)
	}
	// A call for a choice that was never offered is refused before anything is journaled.
	forged := json.RawMessage(`{"@call":"genui_action","catalogId":"urn:hollis-labs:genui:catalog:1","args":{"genui_action":{"id":"made-up","subject":{"kind":"surface.answer","id":"x"},"basis":{"revision":"1"},"risk":"low"},"input":{"choice":"delete-everything"}}}`)
	if code := do(t, srv, "POST", "/api/genui/actions", call(a.Envelope.ID, forged), nil); code == 200 {
		t.Error("a choice the surface never offered was accepted")
	}

	// A high-risk choice waits for the owner's second step.
	var h shown
	do(t, srv, "POST", "/api/genui/surfaces", `{"pattern":"approval","params":{"title":"Delete the archive?","choices":[{"id":"delete","label":"Delete","risk":"high"}]}}`, &h)
	risky := h.Envelope.Data.CreateSurface.Components[0].Props.Choices[0].CallFunction
	first := call(h.Envelope.ID, risky)
	res = response{}
	do(t, srv, "POST", "/api/genui/actions", first, &res)
	if res.AgentFunctionResponse.Value.Status != "needs_confirmation" || card(h.Card).Status != "" {
		t.Fatalf("a high-risk choice: %+v, card %+v; want needs_confirmation and the card still open", res, card(h.Card))
	}
	var proof struct {
		Proof string `json:"proof"`
	}
	if code := do(t, srv, "POST", "/api/genui/confirm", first, &proof); code != 200 || proof.Proof == "" {
		t.Fatalf("confirm: %d", code)
	}
	var fn map[string]any
	_ = json.Unmarshal(risky, &fn)
	fn["args"].(map[string]any)["genui_action"].(map[string]any)["confirmation"] = proof.Proof
	withProof, _ := json.Marshal(fn)
	res = response{}
	do(t, srv, "POST", "/api/genui/actions", call(h.Envelope.ID, withProof), &res)
	if res.AgentFunctionResponse.Value.Status != "ok" || card(h.Card).Status != "submitted" {
		t.Errorf("after confirming: %+v, card %+v", res, card(h.Card))
	}

	// A form: the draft travels with the answer.
	var f shown
	do(t, srv, "POST", "/api/genui/surfaces", `{"pattern":"form","params":{"title":"New note","fields":[{"key":"name","label":"Name","type":"text","required":true},{"key":"pinned","label":"Pinned","type":"checkbox","required":false}]}}`, &f)
	var submit map[string]any
	_ = json.Unmarshal(f.Envelope.Data.CreateSurface.Components[0].Props.Submit.CallFunction, &submit)
	submit["args"].(map[string]any)["input"].(map[string]any)["draft"] = map[string]any{"name": "Ideas", "pinned": true}
	withDraft, _ := json.Marshal(submit)
	res = response{}
	do(t, srv, "POST", "/api/genui/actions", call(f.Envelope.ID, withDraft), &res)
	c := card(f.Card)
	draft, _ := c.Outcome.Data["draft"].(map[string]any)
	if res.AgentFunctionResponse.Value.Status != "ok" || c.Status != "submitted" || draft["name"] != "Ideas" || draft["pinned"] != true {
		t.Errorf("the form: %+v, card %+v", res, c)
	}

	// A list: picking an item gets a new surface with its detail; the old card is replaced.
	var l shown
	do(t, srv, "POST", "/api/genui/surfaces", `{"pattern":"list_detail","params":{"title":"Notes","items":[{"id":"a","title":"Alpha"},{"id":"b","title":"Beta"}],"details":{"b":{"title":"Beta","body":"The second."}}}}`, &l)
	var picked struct {
		Envelope struct {
			ID string `json:"id"`
		} `json:"envelope"`
	}
	if code := do(t, srv, "POST", "/api/genui/surfaces/"+l.Envelope.ID+"/select", `{"id":"b"}`, &picked); code != 200 || picked.Envelope.ID == "" || picked.Envelope.ID == l.Envelope.ID {
		t.Fatalf("select: %d %+v", code, picked)
	}
	if c := card(l.Card); c.Status != "superseded" {
		t.Errorf("the list's first card: %+v, want superseded", c)
	}
}
