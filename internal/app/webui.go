package app

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"git.fractalqb.de/fractalqb/goxic"
	"git.fractalqb.de/fractalqb/nmconv"
)

type Screen struct {
	*goxic.Template
	Theme   goxic.PhIdxs
	InitHdr goxic.PhIdxs
}

type DateTime time.Time

func (dt DateTime) MarshalJSON() ([]byte, error) {
	year, month, day := time.Time(dt).Date()
	hour, min, _ := time.Time(dt).Clock()
	var buf bytes.Buffer
	_, err := fmt.Fprintf(&buf, `{"Date":"%04d-%02d-%02d", "Time":"%02d:%02d"}`,
		year, month, day,
		hour, min)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (dt DateTime) UnmarshalJSON(data []byte) error {
	var tmp struct{ Date, Time string }
	err := json.Unmarshal(data, &tmp)
	if err != nil {
		return err
	}
	t, err := time.Parse(time.RFC3339, tmp.Date+"T"+tmp.Time)
	if err != nil {
		return err
	}
	dt = DateTime(t)
	return nil
}

type WebPage struct {
	*goxic.Template
	Title goxic.PhIdxs
	Style goxic.PhIdxs
	Main  goxic.PhIdxs
}

func (page *WebPage) from(scrnFile, scrnLang string) map[string]*goxic.Template {
	tScr := App.tmpLd.load(scrnFile, scrnLang)
	mustBount := func(subTmpl string) *goxic.BounT {
		t := tScr[subTmpl]
		if t == nil {
			log.Fatala("no `subtemplate` in `screent template`", subTmpl, scrnFile)
		}
		return t.NewBounT(nil)
	}
	mayBount := func(subTmpl string) goxic.Content {
		t := tScr[subTmpl]
		if t == nil {
			return goxic.Empty
		}
		return t.NewBounT(nil)
	}
	btPage := page.NewBounT(nil)
	btPage.Bind(page.Title, mustBount("title"))
	btPage.Bind(page.Style, mayBount("style"))
	btPage.Bind(page.Main, mustBount("main"))
	tmp := btPage.Fixate()
	err := tmp.XformPhs(true, goxic.StripPath)
	if err != nil {
		log.Fatale(err)
	}
	tmp.NewBounT(btPage)
	btPage.BindPName("lang", App.Lang)
	tmp = btPage.Fixate()
	err = tmp.XformPhs(true, goxic.StripPath)
	if err != nil {
		log.Fatale(err)
	}
	tScr[""] = tmp.Pack()
	return tScr
}

var (
	webUiUpd chan Change
	gxName   = nmconv.Conversion{
		Norm:   nmconv.Uncamel,
		Xform:  nmconv.PerSegment(strings.ToLower),
		Denorm: nmconv.Sep(nmconv.Lisp),
	}
)

func webLoadTmpls() {
	var gxtPage WebPage
	ts := App.tmpLd.load("page.html", "")
	goxic.MustIndexMap(&gxtPage, ts[""], false, gxName.Convert)
	scrnShips.loadTmpl(&gxtPage)
	scrnInSys.loadTmpl(&gxtPage)
	scrnMats.loadTmpl(&gxtPage)
}

func webAuth(h http.HandlerFunc) http.HandlerFunc {
	if len(App.WebPin) == 0 {
		return h
	}
	return func(wr http.ResponseWriter, rq *http.Request) {
		_, pass, ok := rq.BasicAuth()
		if ok && subtle.ConstantTimeCompare([]byte(pass), []byte(App.WebPin)) != 1 {
			ok = false
			// ConstTimeComp still varies with length
			time.Sleep(time.Duration(300+rand.Intn(300)) * time.Millisecond)
			log.Warna("wrong web-pin from `remote`", rq.RemoteAddr)
			suspectRq(rq)
		}
		if !ok {
			wr.Header().Set("WWW-Authenticate", `Basic realm="Password: BC+ Web PIN"`)
			wr.WriteHeader(401)
			wr.Write([]byte("Unauthorised.\n"))
			return
		}
		h(wr, rq)
	}
}

func webRoutes() {
	htStatic := http.FileServer(http.Dir(filepath.Join(App.assetDir, "s")))
	http.HandleFunc("/s/", webAuth(http.StripPrefix("/s", htStatic).ServeHTTP))
	http.HandleFunc("/ws/app", webAuth(appWs))
	http.HandleFunc("/ws/log", webAuth(logWs))
	http.HandleFunc("/ships", webAuth(scrnShips.ServeHTTP))
	http.HandleFunc("/insys", webAuth(scrnInSys.ServeHTTP))
	http.HandleFunc("/mats", webAuth(scrnMats.ServeHTTP))
	http.HandleFunc("/", webAuth(func(wr http.ResponseWriter, rq *http.Request) {
		http.Redirect(wr, rq, "/story", http.StatusSeeOther)
	}))
}

func runWebUI() {
	webUiUpd = make(chan Change, 64)
	webLoadTmpls()
	webRoutes()
	keyf := filepath.Join(App.dataDir, keyFile)
	crtf := filepath.Join(App.dataDir, certFile)
	lstn := fmt.Sprintf("%s:%d", App.WebAddr, App.WebPort)
	go wuiUpdate()
	log.Infoa("run web ui on `addr`", lstn)
	log.Fatale(http.ListenAndServeTLS(lstn, crtf, keyf, nil))
}

type WuiMsg struct {
	Cmd string
}

type WuiHdr struct {
	Fid  string
	Name string
	Loc  *Location
	Ship *Ship
}

type WuiUpdate struct {
	WuiMsg
	Hdr *WuiHdr     `json:",omitempty"`
	P   interface{} `json:",omitempty"`
}

const wuiChgHdr = ChgCmdr | ChgShip | ChgLoc | ChgPos

func wuiUpdate() {
	log.Infos("running web UI updater")
	defer func() {
		log.Infos("web UI updater terminated")
	}()
	var (
		buf bytes.Buffer
		err error
		hdr WuiHdr
	)
	enc := json.NewEncoder(&buf)
	for upd := range webUiUpd {
		if webApp.wsConn == nil {
			log.Traces("drop web UI update for not having a client")
			continue
		}
		updMsg := WuiUpdate{WuiMsg: WuiMsg{Cmd: "upd"}}
		readState(noErr(func() {
			if upd&wuiChgHdr != 0 {
				hdr.Fid = cmdr.Head.Fid
				hdr.Name = cmdr.Head.Name
				hdr.Loc = &cmdr.Head.Loc
				hdr.Ship = &cmdr.Head.Ship
				updMsg.Hdr = &hdr
			}
			if upd&WuiUpInSys == WuiUpInSys {
				updMsg.P = &inSysInfo
			}
			err = enc.Encode(&updMsg)
		}))
		if err != nil {
			log.Errora("encoding ui update `err`", err)
		} else {
			log.Debuga("send web UI update for `parts`", upd)
			_, err = webApp.Write(buf.Bytes())
			buf.Reset()
			if err != nil {
				log.Errora("send ui update `err`", err)
			}
		}
	}
}

const suspHistLen = 8

type SuspHist struct {
	ts [suspHistLen]int64
	wp int
}

var (
	wuiSuspects = make(map[string]*SuspHist)
	wuiSuspLock sync.RWMutex
)

func suspectRq(rq *http.Request) {
	suspicion(rq.RemoteAddr, time.Now().Unix())
}

func suspicion(key string, uxt int64) {
	wuiSuspLock.Lock()
	defer wuiSuspLock.Unlock()
	hist := wuiSuspects[key]
	if hist == nil {
		hist = &SuspHist{wp: 1}
		hist.ts[0] = uxt
		wuiSuspects[key] = hist
	} else {
		hist.ts[hist.wp] = uxt
		hist.wp++
		if hist.wp >= suspHistLen {
			hist.wp = 0
		}
	}
}