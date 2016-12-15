package main

import (
	"github.com/BurntSushi/toml"
	"github.com/syndtr/goleveldb/leveldb"
	"github.com/vaughan0/go-ini"

	"crypto/tls"
	"encoding/json"
	"io/ioutil"
	"net/http"
	"os/exec"
	"path/filepath"

	"bytes"
	"fmt"
	"os"
	"time"
)

type Config struct {
	Name         string   `toml:"name"`
	AccessToken  string   `toml:"token"`
	Repositories []string `toml:"repositories"`
	PollingTime  int      `toml:"polling"`
}

type PullRequest struct {
	Id       int                    `json:"id"`
	Title    string                 `json:"title"`
	Assignee map[string]interface{} `json:"assignee"`
	Number   int                    `json:"number"`
	Url      string                 `json:"html_url"`
}

const GITHUB_APIBASE = "https://api.github.com"
const GITHUB_API_LIMIT = 5000
const CONFIG_DIR = ".github_assinee_notifiler"

var db *leveldb.DB
var config *Config
var baseDir string

func init() {
	var err error
	baseDir = filepath.Join(os.Getenv("HOME"), CONFIG_DIR)
	configPath := filepath.Join(baseDir, "config")
	config = &Config{}

	if len(os.Args) > 1 && os.Args[1] == "init" {
		if _, err := os.Stat(baseDir); err == nil {
			fmt.Printf("\033[32mSettings already initialized. If you want to change at %s files and edit them.\033[0m\n", baseDir)
			os.Exit(0)
		}
		initializeSettings()
		os.Exit(0)
	}

	if s, err := os.Stat(baseDir); err != nil {
		config = initializeSettings()
	} else if !s.IsDir() {
		panic(fmt.Sprintf("\033[31m[ERROR] basedir %s found, but not directory!\033[0m\n", baseDir))
	} else {
		if _, err = os.Stat(filepath.Join(configPath)); err != nil {
			panic(err)
		}

		if _, err = toml.DecodeFile(configPath, config); err != nil {
			panic(err)
		}
	}

	ok := true
	if config.AccessToken == "" {
		fmt.Printf(
			"\033[31mGithub access token is empty. Please open %s your editor and put 'token' section.\033[0m\n",
			configPath,
		)
		ok = false
	}
	if config.Name == "" {
		fmt.Printf(
			"\033[31mGithub username is empty. Please open %s your editor and put 'name' section.\033[0m\n",
			configPath,
		)
		ok = false
	}
	if len(config.Repositories) == 0 {
		fmt.Printf(
			"\033[31mWatching repositories are emoty. Please open %s your editor and put 'repositories' section.\033[0m\n",
			configPath,
		)
		ok = false
	}

	if (3600/config.PollingTime)*len(config.Repositories) > GITHUB_API_LIMIT {
		fmt.Printf(
			"\033[93mNumber of %d repositories, and polling by %s sec may be over the Github API LIMIT.\033[0m\n",
			len(config.Repositories),
			config.PollingTime,
		)
	}

	db, err = leveldb.OpenFile(filepath.Join(baseDir, "db"), nil)
	if err != nil {
		panic(err)
	}

	if !ok {
		fmt.Println("Aborted.")
		os.Exit(0)
	}

}

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

	fmt.Println("Auto generated setting files.")
	return config
}

func main() {
	defer db.Close()

	wait := make(chan struct{}, 0)

	for i, r := range config.Repositories {
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

	<-wait
}

func watchPullRequests(repo string) {
	fmt.Println("\033[90mWatch pull requests:", repo, "\033[0m")

	req, err := http.NewRequest("GET", fmt.Sprintf("%s/repos/%s/pulls", GITHUB_APIBASE, repo), nil)
	if err != nil {
		fmt.Println("\033[90m[ERROR]", err, "\033[0m")
		return
	}
	req.Header.Add("Authorization", "token "+config.AccessToken)
	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("\033[90m[ERROR]", err, "\033[0m")
		return
	}
	if resp.StatusCode != 200 {
		fmt.Println("\033[90m[ERROR] HTTP response failed:", resp.StatusCode, "\033[0m")
		return
	}
	defer resp.Body.Close()
	buf, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("\033[90m[ERROR]", err, "\033[0m")
		return
	}

	var list = make([]PullRequest, 0)
	if err := json.Unmarshal(buf, &list); err != nil {
		fmt.Println("\033[90m[ERROR]", err, "\033[0m")
		return
	}

	for _, pr := range list {
		if fmtin, ok := pr.Assignee["fmtin"]; !ok || fmtin.(string) != config.Name {
			continue
		}
		key := []byte(fmt.Sprint(pr.Id))
		if _, err := db.Get(key, nil); err == nil {
			continue
		}
		fmt.Printf("\033[926mAssigned PR found: #%d %s %s\033[0m\n", pr.Number, pr.Title, pr.Url)
		if err := notify(pr); err != nil {
			continue
		}
		db.Put(key, key, nil)
	}
}

func notify(pr PullRequest) error {
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
