package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeAPI is an in-memory CloudXino API, enough for the provider's resources. It normalizes input
// the way the real API does (firewall ports and addresses, key comments), so tests catch diffs.
type fakeAPI struct {
	mu      sync.Mutex
	seq     int
	keys    map[string]map[string]any
	fws     map[string]map[string]any
	zones   map[string]map[string]any
	records map[string]map[string]any // id -> record (with "zone_id")
	servers map[string]map[string]any
	polls   map[string]int // server id -> GETs since a change (drives status transitions)
	lbs     map[string]map[string]any
	srv     *httptest.Server
}

func newFakeAPI(t *testing.T) *fakeAPI {
	f := &fakeAPI{
		keys: map[string]map[string]any{}, fws: map[string]map[string]any{}, zones: map[string]map[string]any{},
		records: map[string]map[string]any{}, servers: map[string]map[string]any{}, polls: map[string]int{},
		lbs: map[string]map[string]any{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	t.Setenv("CLOUDXINO_TOKEN", "cxk_test")
	t.Setenv("CLOUDXINO_API_URL", f.srv.URL+"/api")
	waitInterval = 1
	return f
}

func (f *fakeAPI) id(prefix string) string {
	f.seq++
	return fmt.Sprintf("%s-%04d", prefix, f.seq)
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func notFound(w http.ResponseWriter) {
	reply(w, 404, map[string]any{"detail": "Not found", "code": "not_found"})
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
	route := r.Method + " " + parts[0]
	switch {
	case parts[0] == "users" && parts[1] == "me":
		reply(w, 200, map[string]any{"id": "u1", "email": "dev@example.com", "organization": map[string]any{"id": "o1", "name": "Acme", "role": "owner"},
			"api_key": map[string]any{"name": "ci", "role": "developer"}})
	case route == "GET projects":
		reply(w, 200, []map[string]any{{"id": "p1", "name": "Default", "is_default": true}, {"id": "p2", "name": "Staging", "is_default": false}})
	case parts[0] == "ssh-keys":
		f.sshKeys(w, r, parts, body)
	case parts[0] == "firewalls":
		f.firewalls(w, r, parts, body)
	case parts[0] == "dns":
		f.dns(w, r, parts[1:], body)
	case parts[0] == "mc2":
		f.mc2(w, r, parts, body)
	case parts[0] == "load-balancers":
		f.loadBalancers(w, r, parts, body)
	default:
		notFound(w)
	}
}

func (f *fakeAPI) sshKeys(w http.ResponseWriter, r *http.Request, parts []string, body map[string]any) {
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			out := []map[string]any{}
			for _, k := range f.keys {
				if n := r.URL.Query().Get("name"); n != "" && k["name"] != n {
					continue
				}
				out = append(out, k)
			}
			reply(w, 200, out)
		case "POST":
			fields := strings.Fields(body["public_key"].(string))
			k := map[string]any{"id": f.id("key"), "name": body["name"], "public_key": fields[0] + " " + fields[1], "fingerprint": "SHA256:" + fields[1][len(fields[1])-6:]}
			f.keys[k["id"].(string)] = k
			reply(w, 201, k)
		}
		return
	}
	k, ok := f.keys[parts[1]]
	if !ok {
		notFound(w)
		return
	}
	switch r.Method {
	case "GET":
		reply(w, 200, k)
	case "PATCH":
		k["name"] = body["name"]
		reply(w, 200, k)
	case "DELETE":
		delete(f.keys, parts[1])
		w.WriteHeader(204)
	}
}

func normRules(v any, key string) []map[string]any {
	out := []map[string]any{}
	list, _ := v.([]any)
	for _, raw := range list {
		r := raw.(map[string]any)
		proto := r["protocol"].(string)
		ports, _ := r["ports"].(string)
		var addrs []string
		for _, a := range r[key].([]any) {
			addrs = append(addrs, a.(string))
		}
		out = append(out, map[string]any{"protocol": proto, "ports": canonicalPorts(ports, proto), key: canonicalAddresses(addrs)})
	}
	return out
}

func strList(v any) []string {
	out := []string{}
	if list, ok := v.([]any); ok {
		for _, x := range list {
			out = append(out, x.(string))
		}
	}
	return out
}

func (f *fakeAPI) firewalls(w http.ResponseWriter, r *http.Request, parts []string, body map[string]any) {
	if len(parts) == 1 {
		fw := map[string]any{"id": f.id("fw"), "name": body["name"], "status": "active", "server_count": 0,
			"inbound_rules": normRules(body["inbound_rules"], "sources"),
			"server_ids":    strList(body["server_ids"]), "tags": strList(body["tags"])}
		if body["outbound_rules"] == nil {
			fw["outbound_rules"] = []map[string]any{{"protocol": "tcp", "ports": "all", "destinations": []string{"0.0.0.0/0"}}}
		} else {
			fw["outbound_rules"] = normRules(body["outbound_rules"], "destinations")
		}
		f.fws[fw["id"].(string)] = fw
		reply(w, 201, fw)
		return
	}
	fw, ok := f.fws[parts[1]]
	if !ok {
		notFound(w)
		return
	}
	switch r.Method {
	case "GET":
		reply(w, 200, fw)
	case "PATCH":
		if v, ok := body["name"]; ok {
			fw["name"] = v
		}
		if v, ok := body["inbound_rules"]; ok {
			fw["inbound_rules"] = normRules(v, "sources")
		}
		if v, ok := body["outbound_rules"]; ok {
			fw["outbound_rules"] = normRules(v, "destinations")
		}
		if v, ok := body["server_ids"]; ok {
			fw["server_ids"] = strList(v)
		}
		if v, ok := body["tags"]; ok {
			fw["tags"] = strList(v)
		}
		reply(w, 200, fw)
	case "DELETE":
		delete(f.fws, parts[1])
		reply(w, 200, map[string]any{"message": "deleted"})
	}
}

func (f *fakeAPI) zoneView(z map[string]any, rec map[string]any) map[string]any {
	out := map[string]any{}
	for k, v := range z {
		out[k] = v
	}
	recs := []map[string]any{}
	for _, rr := range f.records {
		if rr["zone_id"] == z["id"] {
			recs = append(recs, rr)
		}
	}
	out["records"] = recs
	if rec != nil {
		out["record"] = rec
	}
	return out
}

func (f *fakeAPI) dns(w http.ResponseWriter, r *http.Request, parts []string, body map[string]any) {
	// parts: zones[/id[/records[/rid]]]
	if len(parts) == 1 {
		if r.Method == "POST" {
			z := map[string]any{"id": f.id("zone"), "name": strings.ToLower(strings.TrimSuffix(body["name"].(string), ".")), "status": "pending",
				"nameservers": []string{"dns1.cloudxino.com", "dns2.cloudxino.com"}}
			f.zones[z["id"].(string)] = z
			reply(w, 201, f.zoneView(z, nil))
			return
		}
		out := []map[string]any{}
		for _, z := range f.zones {
			out = append(out, z)
		}
		reply(w, 200, out)
		return
	}
	z, ok := f.zones[parts[1]]
	if !ok {
		notFound(w)
		return
	}
	if len(parts) == 2 {
		if r.Method == "DELETE" {
			delete(f.zones, parts[1])
			reply(w, 200, map[string]any{"message": "deleted"})
			return
		}
		reply(w, 200, f.zoneView(z, nil))
		return
	}
	setRec := func(rec map[string]any) {
		name, _ := body["name"].(string)
		if name == "" {
			name = "@"
		}
		rec["name"], rec["type"], rec["content"] = strings.ToLower(name), strings.ToUpper(body["type"].(string)), body["content"]
		rec["ttl"] = body["ttl"]
		if rec["ttl"] == nil {
			rec["ttl"] = 3600
		}
		rec["priority"] = body["priority"]
		rec["fqdn"] = strings.TrimPrefix(strings.ToLower(name)+".", "@.") + z["name"].(string)
	}
	if len(parts) == 3 && r.Method == "POST" {
		rec := map[string]any{"id": f.id("rec"), "zone_id": z["id"]}
		setRec(rec)
		f.records[rec["id"].(string)] = rec
		reply(w, 201, f.zoneView(z, rec))
		return
	}
	rec, ok := f.records[parts[3]]
	if !ok || rec["zone_id"] != z["id"] {
		notFound(w)
		return
	}
	switch r.Method {
	case "GET":
		reply(w, 200, rec)
	case "PATCH":
		setRec(rec)
		reply(w, 200, f.zoneView(z, rec))
	case "DELETE":
		delete(f.records, parts[3])
		reply(w, 200, f.zoneView(z, nil))
	}
}

var sizes = map[string][3]int{"s-1vcpu-1gb": {1, 1024, 25}, "s-2vcpu-2gb": {2, 2048, 50}}

func (f *fakeAPI) mc2(w http.ResponseWriter, r *http.Request, parts []string, body map[string]any) {
	if len(parts) == 1 {
		switch r.Method {
		case "GET":
			out := []map[string]any{}
			for _, s := range f.servers {
				out = append(out, s)
			}
			reply(w, 200, out)
		case "POST":
			if body["size"] == "nope" {
				reply(w, 422, map[string]any{"detail": "Unknown size 'nope'", "code": "validation_error"})
				return
			}
			spec := sizes[body["size"].(string)]
			region, _ := body["region"].(string)
			if region == "" {
				region = "dhaka-1"
			}
			project, _ := body["project_id"].(string)
			if project == "" {
				project = "p1"
			}
			s := map[string]any{"id": f.id("srv"), "name": body["name"], "image": body["image"], "size": body["size"], "region": region,
				"status": "creating", "ip_type": body["ip_type"], "login_method": body["login_method"], "project_id": project,
				"tags": strList(body["tags"]), "ssh_key_ids": strList(body["ssh_key_ids"]), "cpu": spec[0], "memory": spec[1], "disk": spec[2]}
			f.servers[s["id"].(string)] = s
			reply(w, 201, s)
		}
		return
	}
	s, ok := f.servers[parts[1]]
	if !ok {
		notFound(w)
		return
	}
	if len(parts) == 3 && parts[2] == "resize" {
		s["status"], s["resize"], s["size"] = "resizing", map[string]any{"to": body["size"]}, body["size"]
		f.polls[parts[1]] = 0
		reply(w, 200, map[string]any{"message": "Resize started", "status": "resizing"})
		return
	}
	switch r.Method {
	case "GET":
		// creating/resizing finish on the second look, deleting disappears on the second look
		f.polls[parts[1]]++
		if f.polls[parts[1]] >= 2 {
			switch s["status"] {
			case "creating", "resizing":
				spec := sizes[s["size"].(string)]
				s["status"], s["resize"], s["ip_address"], s["cpu"], s["memory"], s["disk"] = "running", nil, "203.0.113.20", spec[0], spec[1], spec[2]
			case "deleting":
				delete(f.servers, parts[1])
				notFound(w)
				return
			}
		}
		reply(w, 200, s)
	case "PATCH":
		if v, ok := body["name"]; ok {
			s["name"] = v
		}
		if v, ok := body["tags"]; ok {
			s["tags"] = strList(v)
		}
		reply(w, 200, s)
	case "DELETE":
		s["status"] = "deleting"
		f.polls[parts[1]] = 0
		reply(w, 200, map[string]any{"message": "CC2 instance deletion started"})
	}
}

func (f *fakeAPI) loadBalancers(w http.ResponseWriter, r *http.Request, parts []string, body map[string]any) {
	set := func(lb map[string]any) {
		for _, k := range []string{"name", "algorithm", "forwarding_rules", "health_check", "target_ids"} {
			if v, ok := body[k]; ok && v != nil {
				lb[k] = v
			}
		}
		if lb["health_check"] == nil {
			lb["health_check"] = map[string]any{"protocol": "tcp", "port": nil, "path": "/"}
		}
		// like the real API: a check without a port uses the first rule's target port
		if hc := lb["health_check"].(map[string]any); hc["port"] == nil {
			if rules, _ := lb["forwarding_rules"].([]any); len(rules) > 0 {
				hc["port"] = rules[0].(map[string]any)["target_port"]
			}
		}
		if lb["algorithm"] == nil {
			lb["algorithm"] = "round_robin"
		}
		for _, raw := range lb["forwarding_rules"].([]any) {
			rule := raw.(map[string]any)
			if rule["target_protocol"] == nil {
				rule["target_protocol"] = rule["entry_protocol"]
			}
		}
	}
	if len(parts) == 1 {
		lb := map[string]any{"id": f.id("lb"), "region": "dhaka-1", "status": "active", "ip_address": "203.0.113.50", "target_ids": []string{}}
		set(lb)
		f.lbs[lb["id"].(string)] = lb
		reply(w, 201, lb)
		return
	}
	lb, ok := f.lbs[parts[1]]
	if !ok {
		notFound(w)
		return
	}
	switch r.Method {
	case "GET":
		reply(w, 200, lb)
	case "PATCH":
		set(lb)
		reply(w, 200, lb)
	case "DELETE":
		delete(f.lbs, parts[1])
		reply(w, 200, map[string]any{"message": "deleted"})
	}
}
