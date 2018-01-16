// bcplus project main.go
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"

	gxy "github.com/CmdrVasquess/BCplus/galaxy"
	"github.com/fractalqb/namemap"
	l "github.com/fractalqb/qblog"
)

func init() {
	assetPathRoot = os.Args[0]
	assetPathRoot = filepath.Dir(filepath.Clean(assetPathRoot))
	assetPathRoot = filepath.Join(assetPathRoot, "bcplus.d")
	var err error
	if assetPathRoot, err = filepath.Abs(assetPathRoot); err != nil {
		panic(err)
	}
	glog.Logf(l.Info, "assets: %s", assetPathRoot)
	nmNavItem = namemap.MustLoad(assetPath("nm/navitems.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("nav items", "std → lang:")
	nmRnkCombat = namemap.MustLoad(assetPath("nm/rnk_combat.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("combat ranks", "std → lang:")
	nmRnkTrade = namemap.MustLoad(assetPath("nm/rnk_trade.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("trade ranks", "std → lang:")
	nmRnkExplor = namemap.MustLoad(assetPath("nm/rnk_explore.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("explorer ranks", "std → lang:")
	nmRnkCqc = namemap.MustLoad(assetPath("nm/rnk_cqc.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("CQC ranks", "std → lang:")
	nmRnkFed = namemap.MustLoad(assetPath("nm/rnk_feds.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("federation ranks", "std → lang:")
	nmRnkImp = namemap.MustLoad(assetPath("nm/rnk_imps.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("imperial ranks", "std → lang:")
	nmShipType = namemap.MustLoad(assetPath("nm/shiptype.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("ship types", "std → lang:")
	nmMatType = namemap.MustLoad(assetPath("nm/resctypes.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("rescource types", "std → lang:")
	nmMats = namemap.MustLoad(assetPath("nm/materials.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("materials", "std → lang:")
	nmMatsXRef = nmMats.Base().FromStd().To(false, "wikia")
	nmBdyCats = namemap.MustLoad(assetPath("nm/body-cats.xsx")).
		FromStd().
		To(false, "lang:").
		Verify("materials", "std → lang:")
}

type bcEvent struct {
	source rune
	data   interface{}
}

var theStateLock = sync.RWMutex{}
var theGalaxy *gxy.Galaxy
var theGame = NewGmState()
var eventq = make(chan bcEvent, 128)

var jrnlDir string
var dataDir string

var nmNavItem namemap.FromTo
var nmRnkCombat namemap.FromTo
var nmRnkTrade namemap.FromTo
var nmRnkExplor namemap.FromTo
var nmRnkCqc namemap.FromTo
var nmRnkFed namemap.FromTo
var nmRnkImp namemap.FromTo
var nmShipType namemap.FromTo
var nmMatType namemap.FromTo
var nmMats namemap.FromTo
var nmMatsXRef namemap.FromTo
var nmBdyCats namemap.FromTo

func saveState() {
	if theGame.Cmdr.Name == "" {
		glog.Logf(l.Info, "empty state, nothing to save")
	} else {
		fnm := theGame.Cmdr.Name + ".json"
		fnm = filepath.Join(dataDir, fnm)
		tnm := fnm + "~"
		if w, err := os.Create(tnm); err == nil {
			defer w.Close()
			glog.Logf(l.Info, "save state to %s", fnm)
			err := theGame.save(w)
			w.Close()
			if err != nil {
				glog.Log(l.Error, err)
			} else if err = os.Rename(tnm, fnm); err != nil {
				glog.Log(l.Error, err)
			}
		} else {
			glog.Logf(l.Error, "cannot save game status to '%s': %s", fnm, err)
		}
	}
	theGalaxy.Close()
}

func loadState(cmdrNm string) bool {
	fnm := fmt.Sprintf("%s.json", cmdrNm)
	fnm = filepath.Join(dataDir, fnm)
	glog.Logf(l.Info, "load state from %s", fnm)
	if r, err := os.Open(fnm); os.IsNotExist(err) {
		return false
	} else if err == nil {
		defer r.Close()
		theGame.load(r)
		return true
	} else {
		panic("load commander: " + err.Error())
	}
}

var assetPathRoot string

func assetPath(relPathSlash string) string {
	relPathSlash = filepath.FromSlash(relPathSlash)
	return filepath.Join(assetPathRoot, relPathSlash)
}

const (
	esrcJournal = 'j'
	esrcUsr     = 'u'
)

func eventLoop() {
	glog.Log(l.Info, "starting main event loop…")
	for e := range eventq {
		switch e.source {
		case esrcJournal:
			DispatchJournal(&theStateLock, theGame, e.data.([]byte))
		case esrcUsr:
			DispatchUser(&theStateLock, theGame, e.data.(map[string]interface{}))
		}
	}
}

func main() {
	flag.StringVar(&dataDir, "d", defaultDataDir(),
		"directory to store BC+ data")
	flag.StringVar(&jrnlDir, "j", defaultJournalDir(),
		"directory with journal files")
	flag.UintVar(&webGuiPort, "p", 1337,
		"web GUI port")
	pun := flag.Bool("l", false, "pickup newest existing log")
	verbose := flag.Bool("v", false, "verbose logging")
	verybose := flag.Bool("vv", false, "very verbose logging")
	flag.BoolVar(&acceptHistory, "hist", false, "accept historic events")
	loadCmdr := flag.String("cmdr", "", "preload commander")
	showHelp := flag.Bool("h", false, "show help")
	flag.Parse()
	if *showHelp {
		BCpDescribe(os.Stdout)
		fmt.Println()
		flag.Usage()
		os.Exit(0)
	}
	if *verybose {
		glog.SetLevel(l.Trace)
	} else if *verbose {
		glog.SetLevel(l.Debug)
	}
	glog.Logf(l.Info, "Bordcomputer+ running on: %s\n", runtime.GOOS)
	glog.Logf(l.Info, "data    : %s\n", dataDir)
	var err error
	if _, err = os.Stat(dataDir); os.IsNotExist(err) {
		glog.Fatal("data dir does not exist")
	}
	glog.Logf(l.Info, "journals: %s\n", jrnlDir)
	theGalaxy, err = gxy.OpenGalaxy(
		filepath.Join(dataDir, "systems.json"),
		assetPath("data/"))
	if err != nil {
		glog.Fatal(err)
	}
	if len(*loadCmdr) > 0 {
		loadState(*loadCmdr)
	}
	stopWatch := make(chan bool)
	go WatchJournal(stopWatch, *pun, jrnlDir, func(line []byte) {
		cpy := make([]byte, len(line))
		copy(cpy, line)
		eventq <- bcEvent{esrcJournal, cpy}
	})
	go eventLoop()
	runWebGui()
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	<-sigs
	stopWatch <- true
	glog.Log(l.Info, "BC+ interrupted")
	theStateLock.RLock()
	saveState()
	theStateLock.RUnlock()
	glog.Log(l.Info, "bye…")
}