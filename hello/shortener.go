package hello

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"

	"appengine"
	"appengine/datastore"
	"appengine/user"
)

const UrlKind = "shurls"

func init() {
	http.HandleFunc("/shortener/admin/shorten", shorten)
	http.HandleFunc("/shortener/unshorten", unshorten)
}

type ShortRec struct {
	Url   string
	Short string
}

func jsonOrPanic(i interface{}) []byte {
	buf, err := json.Marshal(i)
	if err != nil {
		panic(fmt.Sprintf("JSON failed: %s\n", err.Error()))
	}
	return buf
}

func respond(code int, w http.ResponseWriter, i interface{}) {
	w.Header().Set("Content-type", "application/json")
	w.WriteHeader(code)
	w.Write(jsonOrPanic(i))
}

func respondHtml(code int, w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-type", "text/html")
	w.WriteHeader(code)
	w.Write([]byte(fmt.Sprintf(
		"<html><head><title>%s</title></head><body>%s</body></html>",
		title, body)))
}

type Msi map[string]interface{}

func paramFromUrlOrBody(r *http.Request, urlKey string) (_ string, ok bool) {
	bodyBytes, err := ioutil.ReadAll(r.Body)
	if err != nil {
		panic("Read failed")
	}
	if len(bodyBytes) != 0 {
		return string(bodyBytes), true
	} else if val := r.URL.Query().Get(urlKey); val != "" {
		return val, true
	}
	return "", false
}

func requireAuth(w http.ResponseWriter, r *http.Request,
	ctx appengine.Context) bool {

	u := user.Current(ctx)
	if u == nil {
		url, err := user.LoginURL(ctx, r.URL.String())
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return false
		}
		w.Header().Set("Location", url)
		w.WriteHeader(http.StatusFound)
		return false
	}
	ctx.Infof("User: %v", u)
	return true
}

func unshorten(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	ctx := appengine.NewContext(r)

	if !requireAuth(w, r, ctx) {
		return
	}

	short, ok := paramFromUrlOrBody(r, "short")
	if !ok {
		respond(400, w, Msi{"error": "No URL given"})
		return
	}

	results := make([]*ShortRec, 0)
	q := datastore.NewQuery(UrlKind).Filter("Short = ", short)
	_, err := q.GetAll(ctx, &results)
	if err != nil {
		respond(500, w, Msi{"error": "Datastore error: " + err.Error()})
		return
	}
	if len(results) == 0 {
		respond(404, w, Msi{"error": "No such entry"})
	}

	respond(200, w, results[0])
}

func shorten(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	ctx := appengine.NewContext(r)

	if !requireAuth(w, r, ctx) {
		return
	}

	url, ok := paramFromUrlOrBody(r, "url")
	if !ok {
		respond(400, w, Msi{"error": "No URL given"})
		return
	}

	key := datastore.NewKey(ctx, UrlKind, url, 0, nil)

	h := sha256.New()
	h.Write([]byte(url))
	hashBytes := h.Sum(nil)
	hashHex := hex.EncodeToString(hashBytes)

	shortRec := &ShortRec{
		Url:   url,
		Short: hashHex,
	}

	_, err := datastore.Put(ctx, key, shortRec)
	if err != nil {
		respond(500, w,
			Msi{"error": fmt.Sprintf("Failed datastore save: %s", err.Error())})
		return
	}

	respond(201, w, shortRec)
}
