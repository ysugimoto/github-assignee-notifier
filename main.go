package main

import (
	"github.com/BurntSushi/toml"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/vaughan0/go-ini"

	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"

	"bytes"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

// Application Configuration
type Config struct {
	Name         string   `toml:"name"`
	AccessToken  string   `toml:"token"`
	Repositories []string `toml:"repositories"`
	PollingTime  int      `toml:"polling"`
	Repeat       uint64   `toml:"repeat"`
}

// Pull Request data
type PullRequest struct {
	Id       int                    `json:"id"`
	Title    string                 `json:"title"`
	Assignee map[string]interface{} `json:"assignee"`
	Number   int                    `json:"number"`
	Url      string                 `json:"html_url"`
}

// Comment data
type Comment struct {
	Id   int    `json:"id"`
	Body string `json:"body"`
	Url  string `json:"html_url"`
}

// Reviewer data
type Reviewer struct {
	Id   int    `json:"id"`
	Name string `json:"login"`
}

// Ansi colors
const (
	RED    = "\033[31m"
	YELLOW = "\033[93m"
	DARK   = "\033[90m"
	GREEN  = "\033[92m"
	BLUE   = "\033[96m"
	RESET  = "\033[0m"
)

// Logger
type Logger struct{}

func (l Logger) Write(message string) {
	fmt.Println(message)
}
func (l Logger) Passive(message string) {
	fmt.Println(DARK, message, RESET)
}
func (l Logger) Error(message string) {
	fmt.Println(DARK, message, RESET)
}
func (l Logger) Warn(message string) {
	fmt.Println(YELLOW, message, RESET)
}
func (l Logger) Notify(message string) {
	fmt.Println(BLUE, message, RESET)
}
func (l Logger) Success(message string) {
	fmt.Println(GREEN, message, RESET)
}

const GITHUB_APIBASE = "https://api.github.com"
const GITHUB_API_LIMIT = 5000
const CONFIG_DIR = ".github_assinee_notifiler"

var db *leveldb.DB
var config *Config
var baseDir string
var logger Logger

var isNocolor *bool
var isJson *bool
var isSilent *bool

func init() {
	var err error
	baseDir = filepath.Join(os.Getenv("HOME"), CONFIG_DIR)
	configPath := filepath.Join(baseDir, "config")
	config = &Config{}
	logger = Logger{}

	// Initialize only
	// e.g. [command] init
	if len(os.Args) > 1 {
		if os.Args[1] == "init" {
			if _, err := os.Stat(baseDir); err == nil {
				logger.Success(fmt.Sprintf("Settings already initialized. If you want to change at %s files and edit them.", baseDir))
				os.Exit(0)
			}
			initializeSettings()
			os.Exit(0)
		}
	}

	// Check settings directory or create it
	if s, err := os.Stat(baseDir); err != nil {
		config = initializeSettings()
	} else if !s.IsDir() {
		logger.Error(fmt.Sprintf("[ERROR] basedir %s found, but not directory!", baseDir))
		os.Exit(1)
	} else {
		if _, err = os.Stat(filepath.Join(configPath)); err != nil {
			panic(err)
		}
		if _, err = toml.DecodeFile(configPath, config); err != nil {
			panic(err)
		}
	}

	// Validate config values
	ok := true
	if config.AccessToken == "" {
		logger.Error(
			fmt.Sprintf("Github access token is empty. Please open %s your editor and put 'token' section.", configPath),
		)
		ok = false
	}
	if config.Name == "" {
		logger.Error(
			fmt.Sprintf("Github username is empty. Please open %s your editor and put 'name' section.", configPath),
		)
		ok = false
	}
	if len(config.Repositories) == 0 {
		logger.Error(
			fmt.Sprintf("Watching repositories are emoty. Please open %s your editor and put 'repositories' section.", configPath),
		)
		ok = false
	}

	// Calculate watch repositories to avoid over the API limit rate
	if (3600/config.PollingTime)*len(config.Repositories) > GITHUB_API_LIMIT {
		logger.Warn(
			fmt.Sprintf("Number of %d repositories, and polling by %d sec may be over the Github API LIMIT.", len(config.Repositories), config.PollingTime),
		)
	}

	// Open LevelDB
	db, err = leveldb.OpenFile(filepath.Join(baseDir, "db"), nil)
	if err != nil {
		panic(err)
	}

	if !ok {
		fmt.Println("Aborted.")
		os.Exit(0)
	}

}

// Initialize settings
// @return *Config
func initializeSettings() *Config {
	if err := os.Mkdir(baseDir, 0755); err != nil {
		panic(err)
	}
	if err := os.Mkdir(filepath.Join(baseDir, "db"), 0755); err != nil {
		panic(err)
	}

	config := &Config{
		Repositories: make([]string, 0),
		PollingTime:  30,
		Repeat:       5 * 60,
	}
	git, err := ini.LoadFile(filepath.Join(os.Getenv("HOME"), ".gitconfig"))
	if err == nil {
		if n, ok := git.Get("user", "name"); ok {
			config.Name = n
		}
	}

	buf := new(bytes.Buffer)
	if err := toml.NewEncoder(buf).Encode(config); err != nil {
		panic(err)
	}

	if err := ioutil.WriteFile(filepath.Join(baseDir, "config"), buf.Bytes(), 0600); err != nil {
		panic(err)
	}

	iconPath := filepath.Join(baseDir, "icon.png")
	if _, err = os.Stat(iconPath); err != nil {
		icon, _ := Asset("etc/icon.png")
		if err = ioutil.WriteFile(iconPath, icon, 0644); err != nil {
			panic(err)
		}
	}

	logger.Write("Auto generated setting files.")
	return config
}

func main() {
	isNocolor = flag.Bool("nocolor", false, "No colored output")
	isJson = flag.Bool("json", false, "Message returns JSON string")
	isSilent = flag.Bool("silent", false, "Silent mode: stop notification, output only")

	defer db.Close()

	if len(os.Args) > 1 && os.Args[1] == "summary" {
		from := "yesterday"
		if len(os.Args) > 2 {
			from = os.Args[2]
		}
		showSummary(from)
		return
	}

	wait := make(chan struct{}, 0)

	for i, r := range config.Repositories {
		// Loop and watch PRs in goroutine
		watchPullRequests(r)
		go func(duration int, repo string) {
			time.Sleep(time.Duration(duration*10) * time.Second)
			ticker := time.NewTicker(time.Second * 60)
			for {
				select {
				case <-ticker.C:
					watchPullRequests(repo)
				}
			}
		}(i, r)
	}

	// Blocking
	<-wait
}

func sendRequest(url string, customHeaders map[string]string) ([]byte, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	// Attach access token
	req.Header.Add("Authorization", "token "+config.AccessToken)
	if customHeaders != nil {
		for key, val := range customHeaders {
			req.Header.Add(key, val)
		}
	}
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP response failed: %d, %s", resp.StatusCode, string(buf))
	}

	return buf, nil
}

// Send API reqeust and check assigned you
// @param repo string
func watchPullRequests(repo string) {
	logger.Passive("Watch pull requests: " + repo)

	url := fmt.Sprintf("%s/repos/%s/pulls", GITHUB_APIBASE, repo)
	buf, err := sendRequest(url, nil)
	if err != nil {
		logger.Error("[ERROR] " + err.Error())
		return
	}

	var list = make([]PullRequest, 0)
	if err := json.Unmarshal(buf, &list); err != nil {
		logger.Error("[ERROR] " + err.Error())
		return
	}

	// Loop and check asssignee and mensioned comment
	for _, pr := range list {
		checkIssueComment(repo, pr)
		checkReviewComment(repo, pr)
		checkReviewRequests(repo, pr)
		if login, ok := pr.Assignee["login"]; !ok || login.(string) != config.Name {
			continue
		}
		key := []byte(fmt.Sprint(pr.Id))
		if v, err := db.Get(key, nil); err != nil {
			// Didn't notify?
			if !*isJson {
				logger.Notify(fmt.Sprintf("Assigned PR found: #%d %s %s", pr.Number, pr.Title, pr.Url))
				// send notification in goroutine
				go notify(pr)
			} else {
				buf, _ := json.Marshal(pr)
				logger.Notify(string(buf))
			}
		} else if isReNotify(v) {
			// Need to notify repeatable?
			if !*isJson {
				logger.Warn(fmt.Sprintf("[REPEAT] Assigned PR found: #%d %s %s", pr.Number, pr.Title, pr.Url))
				// send notification in goroutine
				go notify(pr)
			} else {
				buf, _ := json.Marshal(pr)
				logger.Notify(string(buf))
			}
		} else {
			continue
		}

		// Save last notified timestamp
		val := make([]byte, 8)
		binary.LittleEndian.PutUint64(val, uint64(time.Now().Unix()))
		db.Put(key, val, nil)
	}

}

// Check PR's review comments
func checkReviewComment(repo string, pr PullRequest) {
	logger.Passive("Check review comment: " + repo)
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/comments", GITHUB_APIBASE, repo, pr.Number)
	buf, err := sendRequest(url, nil)
	if err != nil {
		logger.Error("[ERROR] " + err.Error())
		return
	}

	var comments = make([]Comment, 0)
	if err := json.Unmarshal(buf, &comments); err != nil {
		logger.Error("[ERROR] " + err.Error())
		return
	}

	for _, c := range comments {
		if !strings.Contains(c.Body, "@"+config.Name) {
			continue
		}
		key := []byte(fmt.Sprintf("review_%d_%d", pr.Number, c.Id))
		if _, err := db.Get(key, nil); err != nil {
			logger.Notify(fmt.Sprintf("Mensioned in PR: %s", c.Url))
			go notifyComment(pr.Number, c.Url)
			db.Put(key, []byte("1"), nil)
		}
	}
}

// Check PR's comments
func checkIssueComment(repo string, pr PullRequest) {
	logger.Passive("Check mensioned comment: " + repo)
	url := fmt.Sprintf("%s/repos/%s/issues/%d/comments", GITHUB_APIBASE, repo, pr.Number)
	buf, err := sendRequest(url, map[string]string{
		"Accept": "application/vnd.github.black-cat-preview+json",
	})
	if err != nil {
		logger.Error("[ERROR] " + err.Error())
		return
	}

	var comments = make([]Comment, 0)
	if err := json.Unmarshal(buf, &comments); err != nil {
		logger.Error("[ERROR] " + err.Error())
		return
	}

	for _, c := range comments {
		if !strings.Contains(c.Body, "@"+config.Name) {
			continue
		}
		key := []byte(fmt.Sprintf("comment_%d_%d", pr.Number, c.Id))
		if _, err := db.Get(key, nil); err != nil {
			logger.Notify(fmt.Sprintf("Mensioned in PR issue: %s", c.Url))
			go notifyComment(pr.Number, c.Url)
			db.Put(key, []byte("1"), nil)
		}
	}
}

// Check you added as reviewer
func checkReviewRequests(repo string, pr PullRequest) {
	logger.Passive("Check review request: " + repo)
	url := fmt.Sprintf("%s/repos/%s/pulls/%d/requested_reviewers", GITHUB_APIBASE, repo, pr.Number)
	buf, err := sendRequest(url, map[string]string{
		"Accept": "application/vnd.github.black-cat-preview+json",
	})
	if err != nil {
		logger.Error("[ERROR] " + err.Error())
		return
	}

	var reviewers = make([]Reviewer, 0)
	if err := json.Unmarshal(buf, &reviewers); err != nil {
		logger.Error("[ERROR] " + err.Error())
		return
	}

	for _, r := range reviewers {
		if r.Name != config.Name {
			continue
		}
		key := []byte(fmt.Sprintf("reviewer_%d_%d", pr.Number, r.Id))
		if _, err := db.Get(key, nil); err != nil {
			logger.Notify(fmt.Sprintf("You added as reviewer in PR: #%d", pr.Number))
			go notifyReviewer(pr)
			db.Put(key, []byte("1"), nil)
		}
	}
}

// Check PR should notify
func isReNotify(v []byte) (is bool) {
	now := uint64(time.Now().Unix())
	last := binary.LittleEndian.Uint64(v)

	if last+config.Repeat < now {
		is = true
	}

	return
}

// Send notification
func notify(pr PullRequest) error {
	if *isSilent {
		return nil
	}
	args := []string{
		"-title",
		fmt.Sprintf("New Pull Request Assigned: #%d", pr.Number),
		"-subtitle",
		pr.Title,
		"-timeout",
		"300",
		"-open",
		pr.Url,
		"-message",
		pr.Url,
		"-appIcon",
		filepath.Join(baseDir, "icon.png"),
	}

	return exec.Command("terminal-notifier", args...).Run()
}

// Send notification for comment
func notifyComment(prId int, url string) error {
	if *isSilent {
		return nil
	}
	args := []string{
		"-title",
		fmt.Sprintf("Mensioned in PR: #%d", prId),
		"-timeout",
		"300",
		"-open",
		url,
		"-message",
		url,
		"-appIcon",
		filepath.Join(baseDir, "icon.png"),
	}

	return exec.Command("terminal-notifier", args...).Run()
}

// Send reviewer notification
func notifyReviewer(pr PullRequest) error {
	if *isSilent {
		return nil
	}
	args := []string{
		"-title",
		fmt.Sprintf("You added reviewer: #%d", pr.Number),
		"-subtitle",
		pr.Title,
		"-timeout",
		"300",
		"-open",
		pr.Url,
		"-message",
		pr.Url,
		"-appIcon",
		filepath.Join(baseDir, "icon.png"),
	}

	return exec.Command("terminal-notifier", args...).Run()
}

// Show summary
func showSummary(from string) {
	switch from {
	case "today":
		from = time.Now().Format("20060102")
	case "yesterday":
		from = time.Now().Add(-time.Hour * 24).Format("20060102")
	}
	var t time.Time
	var err error
	t, err = time.Parse("20060102", from)
	if err != nil {
		logger.Error("Unrecognized summary date. Please input as YYYYMMDD format or reserved string 'today' and 'yesterday'.")
		return
	}
	t = t.Add(-time.Hour * 9)
	query := url.Values{}
	query.Add("since", t.Format("2006-01-02T15:00:00Z"))
	req, err := http.NewRequest("GET", fmt.Sprintf("%s/notifications?%s", GITHUB_APIBASE, query.Encode()), nil)
	if err != nil {
		logger.Error("[ERROR] " + err.Error())
		return
	}
	// Attach access token
	req.Header.Add("Authorization", "token "+config.AccessToken)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	resp, err := client.Do(req)
	defer resp.Body.Close()
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		logger.Error("[ERROR] " + err.Error())
		return
	}
	fmt.Println(string(buf))

}
