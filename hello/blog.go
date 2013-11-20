package hello

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/russross/blackfriday"
	"io/ioutil"
	"net/http"
	"strconv"
	"text/template"
	"time"

	"appengine"
	"appengine/datastore"
)

const BlogKind = "blog"

func init() {
	http.HandleFunc("/blog/admin/create", createBlog)
	http.HandleFunc("/blog/", getBlog)
}

type BlogPost struct {
	Id    int
	Title string    `json:"title"`
	Body  string    `json:"body"`
	Ts    time.Time `json:"ts"`
}

func (p *BlogPost) Validate() (errMsg string) {
	if p.Title == "" {
		return "No title given"
	}
	if p.Body == "" {
		return "No body given"
	}
	if p.Ts.Unix() == 0 {
		return "Timestamp was zero"
	}

	return ""
}

const htmlErrTemplate = `<html>
<head><title>Error</title></head>
<body>{{.Body}}</body>`

func errRespondJson(w http.ResponseWriter, code int, errMsg string) {
	respond(code, w, Msi{"error": errMsg})
}

func errRespondHtml(w http.ResponseWriter, code int, errMsg string) {
	tmpl, err := template.New("wat").Parse(htmlErrTemplate)
	if err != nil {
		w.Write([]byte("Template render failure"))
		return
	}

	var buf bytes.Buffer

	tmpl.Execute(&buf, map[string]string{"Body": errMsg})
	w.Write(buf.Bytes())
}

func requireMethodJson(r *http.Request, w http.ResponseWriter, method string) bool {
	w.Header().Set("Content-type", "application/json")
	if r.Method != method {
		errRespondJson(w, 405, "Expected http method: "+method)
		return false
	}
	return true
}

func requireMethodHtml(r *http.Request, w http.ResponseWriter, method string) bool {
	w.Header().Set("Content-type", "text/html")
	if r.Method != method {
		errRespondHtml(w, 405, "Expected http method: "+method)
		return false
	}
	return true
}

func listBlogs(w http.ResponseWriter, r *http.Request) {
	if !requireMethodHtml(r, w, "GET") {
		return
	}

	ctx := appengine.NewContext(r)

	q := datastore.NewQuery(BlogKind)

	posts := []*BlogPost{}
	iter := q.Run(ctx)
	for {
		var bp BlogPost
		_, err := iter.Next(&bp)
		if err == datastore.Done {
			break
		}
		if err != nil {
			errRespondHtml(w, 500, "Scan error: "+err.Error())
			return
		}
		posts = append(posts, &bp)
	}

	// body := postsToHtml(posts)

	var buf bytes.Buffer

	// buf.WriteString("<html><head><title>Blog posts</title></head><body>\n")
	buf.WriteString("<table border=1>\n")
	for _, bp := range posts {
		buf.WriteString(fmt.Sprintf("<tr><td>%d</td><td>%s</td><td>%s</td></tr>\n",
			bp.Id, bp.Title, bp.Body[0:intMin(len(bp.Body), 20)]))
	}
	buf.WriteString("</table>\n")

	respondHtml(200, w, "Blog posts", string(buf.Bytes()))
}

func intMin(x, y int) int {
	if x < y {
		return x
	}
	return y
}

func getBlog(w http.ResponseWriter, r *http.Request) {
	if !requireMethodHtml(r, w, "GET") {
		return
	}

	id := r.URL.Query().Get("id")
	if id != "" {
		getOneBlog(id, w, r)
	} else {
		listBlogs(w, r)
	}
}

func getOneBlog(id string, w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)

	key := datastore.NewKey(ctx, BlogKind, id, 0, nil)
	var blogPost BlogPost
	// var blogState GlobalBlogState
	if err := datastore.Get(ctx, key, &blogPost); err != nil {
		errRespondHtml(w, 500, "Failed datastore get: "+err.Error())
		return
	}

	// if blogPost == nil {
	// 	errRespondHtml(w, 404, "No such blog post")
	// 	return
	// }

	rendered := blackfriday.MarkdownBasic([]byte(blogPost.Body))
	w.Write([]byte(rendered))
}

func createBlog(w http.ResponseWriter, r *http.Request) {
	ctx := appengine.NewContext(r)
	if !requireMethodJson(r, w, "POST") {
		return
	}

	// bodyBytes, err := ioutil.ReadAll(r.Body)
	// if err != nil {
	// 	panic(err.Error())
	// }
	// ctx.Infof("bodyBytes: %s\n", string(bodyBytes))
	// return

	post := BlogPost{}
	contentType := r.Header.Get("Content-type")
	switch contentType {
	case "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			errRespondHtml(w, 400, "Failed form parse: "+err.Error())
			return
		}
		post.Body = r.Form.Get("body")
		post.Title = r.Form.Get("title")
	case "application/json":
		bodyBytes, err := ioutil.ReadAll(r.Body)
		if err != nil {
			errRespondHtml(w, 500, "Failed reading request body")
			return
		}
		if err := json.Unmarshal(bodyBytes, &post); err != nil {
			errRespondHtml(w, 400, "Invalid JSON")
			return
		}
	default:
		errRespondHtml(w, 400, "Expected JSON or form encoded request, got "+
			contentType)
		return
	}

	blogCtr, err := getAndIncBlogCtr(ctx)
	if err != nil {
		errRespondHtml(w, 500, "Failed counter increment: "+err.Error())
		return
	}

	post.Ts = time.Now()
	post.Id = blogCtr

	if errMsg := post.Validate(); errMsg != "" {
		errRespondHtml(w, 400, "Validation failed: "+errMsg)
		return
	}

	key := datastore.NewKey(ctx, BlogKind, strconv.Itoa(post.Id), 0, nil)
	if _, err := datastore.Put(ctx, key, &post); err != nil {
		errRespondHtml(w, 500, "Failed saving blog post: "+err.Error())
		return
	}

	respondHtml(201, w, "Posted OK", "Posted OK")
}

type GlobalBlogState struct {
	Ctr int
}

func getAndIncBlogCtr(ctx appengine.Context) (int, error) {
	var kind, id string = "globalBlogState", "globalBlogState"
	key := datastore.NewKey(ctx, kind, id, 0, nil)
	blogState := &GlobalBlogState{}
	err := datastore.RunInTransaction(ctx, func(c appengine.Context) error {
		err := datastore.Get(ctx, key, blogState)
		if err != nil && err != datastore.ErrNoSuchEntity {
			return err
		}
		if blogState == nil {
			blogState = &GlobalBlogState{1}
		}
		newBlogState := *blogState
		newBlogState.Ctr++

		if _, err := datastore.Put(c, key, &newBlogState); err != nil {
			return err
		}
		return nil
	}, nil)
	if err != nil {
		return 0, err
	}
	return blogState.Ctr, nil
}
