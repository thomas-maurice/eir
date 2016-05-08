/*
           DO WHAT THE FUCK YOU WANT TO PUBLIC LICENSE
                   Version 2, December 2004

Copyright (C) 2016 Thomas Maurice <thomas@maurice.fr>

Everyone is permitted to copy and distribute verbatim or modified
copies of this license document, and changing it is allowed as long
as the name is changed.

           DO WHAT THE FUCK YOU WANT TO PUBLIC LICENSE
  TERMS AND CONDITIONS FOR COPYING, DISTRIBUTION AND MODIFICATION

 0. You just DO WHAT THE FUCK YOU WANT TO.
*/

package main

import (
    log "github.com/Sirupsen/logrus"
    "github.com/nlopes/slack"
    "github.com/spf13/viper"
    "github.com/gorilla/mux"
    "github.com/spf13/cobra"
    "gopkg.in/yaml.v2"
    "encoding/json"
    "path/filepath"
    "html/template"
    "io/ioutil"
    "net/http"
    "os/exec"
    "strings"
    "bytes"
    "time"
    "fmt"
    "os"
)

// Represents a command structure
// Command is a commandline
// Timeout represents the timeout in seconds
type Command struct {
    Command             string
    Timeout             time.Duration
}

// Action for a given probe or global status change
// Represents a change *to*  given state. For instance OnOk will be triggered
// When the states goes from whatever to OK
type Action struct {
    OnOk                []Command
    OnWarning           []Command
    OnCritical          []Command
    OnUnknown           []Command
}

// Configuration object
type Config struct {
    SlackToken          string
    SlackChannel        string
    StatusFile          string
    ResultDir           string
    WatchInterval       time.Duration
    Debug               bool
    WebHooks            []string
    WebHooksTimeout     time.Duration
    EnableHttpStatus    bool
    HttpListenOn        string
    DryRun              bool
    Actions             struct {
        Global              Action
        Probes              map[string]Action
    }
}

var Conf Config

// Probe result object
type ProbeResult struct {
    Name                string  `yaml:"Name"`
    Status              string  `yaml:"Status"`
    Text                string  `yaml:"Text"`
}
// Status can be one of the following :
// - UNKNOWN
// - OK
// - WARNING
// - CRITICAL
// Unknown is ignored
type ServerState struct {
    Status              string          `yaml:"Status"`
    Hostname            string          `yaml:"Hostname"`
    Version             string          `yaml:"Version"`
    Details             []ProbeResult   `yaml:"Details"`
}

// Compares two server states
func (self *ServerState) Equals(other ServerState) (bool) {
    return self.Status == other.Status
}

// Returns a new ServerState object
func NewServerState() (ServerState) {
    hostname, err := os.Hostname()
    if err != nil {
        hostname = "Unknown hostname"
    }
    return ServerState{Status: "UNKNOWN", Hostname: hostname, Version: Version}
}

// Loads a server state from a file
func (self *ServerState) LoadFromFile(file string) (error) {
    buffer, err := ioutil.ReadFile(file)
    if err != nil {
        var results []ProbeResult
        self.Status = "UNKNOWN"
        self.Details = results
        return err
    }

    err = yaml.Unmarshal(buffer, &self)

    if err != nil {
        var results []ProbeResult
        self.Status = "UNKNOWN"
        self.Details = results
        return err
    }

    return nil
}

// Creates a diff of two states
// Returns an array of the probe results that have changed.
func (self *ServerState) GetProbeDiff(newState *ServerState) ([]ProbeResult) {
    var result []ProbeResult
    for _, oldResult := range self.Details {
        for _, newResult := range newState.Details {
            if oldResult.Name == newResult.Name {
                if oldResult.Status != newResult.Status {
                    result = append(result, newResult)
                }
            }
        }
    }
    return result
}

// Writes a server status to a state file
func (self *ServerState) SaveToFile(file string) (error) {
    buffer, err := yaml.Marshal(&self)
    if err != nil {
        return err
    }
    err = ioutil.WriteFile(file, buffer, 0640)
    return err
}

// Calculates a new status given two statusses
func CalculateNewStatus(one string, two string) (string) {
    if one == two {
        return one
    }

    if one == "UNKNOWN" { return two }
    if two == "UNKNOWN" { return one }
    if one == "CRITICAL" || two == "CRITICAL" { return "CRITICAL" }
    if one == "WARNING" || two == "WARNING" { return "WARNING" }
    if one == "OK" || two == "OK" { return "OK" }
    return "UNKNOWN"
}

// Checks the correctness of a status
func StatusIsValid(status string) bool {
    return (status == "UNKNOWN" || status == "OK" || status == "CRITICAL" || status == "WARNING")
}

// Loads a server status from a result directory
func (self *ServerState) LoadFromDirectory(directory string) (error) {
    files, err := ioutil.ReadDir(directory)
    self.Status = "UNKNOWN"
    if err != nil {
        return err
    }
    log.Info("Loading probe results")
    for _, file := range files {
        filename := filepath.Join(directory, file.Name())
        log.Debug(" * ", filename)
        buffer, err := ioutil.ReadFile(filename)
        if err != nil {
            log.Error(" ! Could not read file ", filename, ": ", err)
            continue
        }
        content := strings.Split(bytes.NewBuffer(buffer).String(), "\n")
        if len(content) == 0 {
            log.Warning(" ? Got empty probe result for ", filename)
            continue
        }
        var result ProbeResult
        if len(content) >= 2 {
            result.Text = strings.TrimRight(strings.Join(content[1:], "\n"), "\n\r ")
        }
        result.Status = content[0]
        result.Name = file.Name()
        if !StatusIsValid(result.Status) {
            log.Warning(" ! Got invalid status for probe ", file.Name(), ": ", result.Status, ". Discarding this result")
            continue
        }
        self.Status = CalculateNewStatus(result.Status, self.Status)
        self.Details = append(self.Details, result)
    }
    return nil
}

// Executes a shell command, and kills it if it does not terminates in time
func ExecuteCommandWithTimeout(command string, timeout time.Duration, dryRun bool) (error) {
    // TODO: Make this configurable
    if timeout == 0 {
        timeout = time.Duration(30)
    }

    timeout = timeout * time.Second
    if dryRun {
        log.Debug("Would have executed `", command, "` with a ", timeout, " timeout (dry run)")
        return nil
    }

    log.Debug("Executing command with ",  timeout, " timeout: ", command)
    cmd := exec.Command("/bin/sh", "-c", command)
    if err := cmd.Start(); err != nil {
        log.Error(err)
        return err
    }
    timer := time.AfterFunc(timeout, func() {
        cmd.Process.Kill()
        log.Warn("Killed command `", command, "` because timeout exceeded (", timeout, ")")
    })

    err := cmd.Wait();
    if err != nil {
        log.Error("Command `" , command, "` failed: ", err)
    }
    timer.Stop()
    return err
}

// Executes the commands in an Action object when a certain state is reached
// The actions are not ran sequencially, the are pretty much ran at
// the same time within different goroutines
func ExecStatusChangeCommands(state string, actions Action, dryRun bool) (error) {
    switch state {
        case "UNKNOWN":
            for _, command := range actions.OnUnknown {
                go ExecuteCommandWithTimeout(command.Command, command.Timeout, dryRun)
            }
        case "CRITICAL":
            for _, command := range actions.OnCritical {
                go ExecuteCommandWithTimeout(command.Command, command.Timeout, dryRun)
            }
        case "WARNING":
            for _, command := range actions.OnWarning {
                go ExecuteCommandWithTimeout(command.Command, command.Timeout, dryRun)
            }
        case "OK":
            for _, command := range actions.OnOk {
                go ExecuteCommandWithTimeout(command.Command, command.Timeout, dryRun)
            }
    }

    return nil
}

// Loads the configuration file and initializes the Conf object
func InitConfig() {
    viper.SetConfigType("yaml")
    viper.AddConfigPath(".")
    viper.AddConfigPath("$HOME")
    viper.AddConfigPath("/etc/eir")

    viper.SetDefault("WebhooksTimeout", 3)
    viper.SetDefault("ResultDir", "./results")
    viper.SetDefault("StatusFile", "./status.yml")
    viper.SetDefault("Debug", false)
    viper.SetDefault("WatchInterval", 10)
    viper.SetDefault("EnableHttpStatus", false)
    viper.SetDefault("HttpListenOn", "127.0.0.1:8080")
    viper.SetDefault("DryRun", false)

    viper.SetConfigName("eir")

    err := viper.ReadInConfig()

    if err != nil {
        log.Fatal("No configuration file loaded ", err)
    }

    err = viper.Unmarshal(&Conf)

    Conf.WatchInterval = Conf.WatchInterval * time.Second
    Conf.WebHooksTimeout = Conf.WebHooksTimeout * time.Second

    if Conf.SlackToken != "" && Conf.SlackChannel == "" {
        log.Warning("You provided a SlackToken but no SlackChannel, Slack notifications will be disabled")
    }

    if Conf.DryRun {
        log.Warning("DryRun mode is active, none of the actions you specified is going to be executed")
    }

    if Conf.Debug == true {
        log.SetLevel(log.DebugLevel)
        log.Debug("Log level set to debug")
    }
}

// Postes a new server status on Slack
func PostStatusOnSlack(client *slack.Client, channel string, newState ServerState, oldState ServerState) (string, string, error) {
    var attachments []slack.Attachment
    var markdown_in []string
    markdown_in = append(markdown_in, "text")
    markdown_in = append(markdown_in, "pretext")
    for _, result := range newState.Details {
        color := "#eeeeee"
        if result.Status == "WARNING" { color = "warning" }
        if result.Status == "CRITICAL" { color = "danger" }
        if result.Status == "OK" { color = "good" }
        attachments = append(attachments, slack.Attachment{Color: color,
            Title: result.Name + " is " + result.Status,
            Text: result.Text,
            MarkdownIn: markdown_in})
    }
    msg := slack.PostMessageParameters{AsUser: true,
        Markdown: true,
        Attachments: attachments}

    ch, ts, err := client.PostMessage(channel, newState.Hostname + " status changed from *" + oldState.Status + "* to *" + newState.Status + "*", msg)
    if err != nil {
        log.Error("Error posting message: ", err)
        return "", "", err
    } else {
        log.Debug("Successfully posted message on ", channel)
    }
    return ch, ts, nil
}

// Serves the status of the server via HTTP
// The return body will just be the serialization in json of
// the appropriate ServerState object.
//
// Status will be read from the latest saved status file, to avoid
// re reading all the probe results.
func serveHttpStatus(w http.ResponseWriter, r *http.Request) {
    log.Info("Got asked to report my status via HTTP")
    currentState := NewServerState()
    currentState.LoadFromFile(Conf.StatusFile)
    jsonBody, err := json.Marshal(&currentState)
    if err != nil {
        log.Error("Could not serialize server state: ", err)
        w.WriteHeader(http.StatusInternalServerError)
        fmt.Fprintln(w, err)
        return
    }
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusOK)
    fmt.Fprintln(w, bytes.NewBuffer(jsonBody).String())
}

// Serves the UI corresponding to the monitoring state
func serveHttpStatusUI(w http.ResponseWriter, r *http.Request) {
    log.Info("Got asked to serve the webui")
    w.Header().Set("Content-Type", "text/html")
    w.WriteHeader(http.StatusOK)
    tmpl, err := template.New("Webui").Parse(WebUITemplate)
    if err != nil {
        fmt.Fprintln(w, err)
        return
    }
    currentState := NewServerState()
    currentState.LoadFromFile(Conf.StatusFile)
    tmpl.Execute(w, currentState)
}

// Commands

var RootCmd = &cobra.Command{
    Use:   "eir",
    Short: "Simple monitoring and healing software",
}

var VersionCmd = &cobra.Command{
    Use:   "version",
    Short: "Print the version number",
    Long:  ``,
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Println("Eir, version", Version)
    },
}

var ConfSampleCmd = &cobra.Command{
    Use:   "confsample",
    Short: "Prints a sample configuration file",
    Long:  ``,
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Println(ConfigSample)
    },
}

var RunCmd = &cobra.Command{
    Use:   "run",
    Short: "Runs the software",
    Long:  `Runs the software's main loop.

    It will look for the configuration file, run it and loop until you stop it.
    If you want to daemonize Eir, you have to do it yourself. Using Systemd for
    instance
    `,
    Run: func(cmd *cobra.Command, args []string) {
        log.Info("Booting up Eir")
        InitConfig()
        if Conf.EnableHttpStatus {
            log.Info("Enabled serving HTTP report on ", Conf.HttpListenOn)
            statusRouter := mux.NewRouter()
            statusRouter.HandleFunc("/", serveHttpStatus)
            statusRouter.HandleFunc("/ui", serveHttpStatusUI)
            go http.ListenAndServe(Conf.HttpListenOn, statusRouter)
        }

        for {
            currentState := NewServerState()
            newState := NewServerState()
            // No error checking because default state is good enough
            currentState.LoadFromFile(Conf.StatusFile)
            err := newState.LoadFromDirectory(Conf.ResultDir)
            if err != nil {
                log.Error("Could not load the server's state: ", err)
            }
            if !newState.Equals(currentState) {
                log.Info("State changed from ", currentState.Status, " to ", newState.Status)
                probesDiff := currentState.GetProbeDiff(&newState)
                for _, probe := range probesDiff {
                    if action, ok := Conf.Actions.Probes[probe.Name]; ok {
                        ExecStatusChangeCommands(probe.Status, action, Conf.DryRun)
                    }
                }
                ExecStatusChangeCommands(newState.Status, Conf.Actions.Global, Conf.DryRun)
                if Conf.SlackToken != "" && Conf.SlackChannel != "" {
                    slackClient := slack.New(Conf.SlackToken)
                    log.Debug("Notifying on Slack")
                    PostStatusOnSlack(slackClient, Conf.SlackChannel, newState, currentState)
                }
                log.Debug("Notifying the webhooks, with a timeout of ", Conf.WebHooksTimeout)
                for _, url := range Conf.WebHooks {
                    log.Debug(" - ", url)
                    jsonBody, err := json.Marshal(&newState)
                    if err != nil {
                        log.Error("Could not serialize server state: ", err)
                        continue
                    }
                    request, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
                    if err != nil {
                        log.Error("Could not create HTTP request: ", err)
                        continue
                    }
                    client := &http.Client{Timeout: Conf.WebHooksTimeout}
                    resp, err := client.Do(request)
                    if err != nil {
                        log.Error("Could not make HTTP request: ", err)
                        continue
                    }
                    resp.Body.Close()
                }
            } else {
                log.Debug("Server status is unchanged")
            }
            err = newState.SaveToFile(Conf.StatusFile)
            if err != nil {
                log.Error("Could not save the server's state: ", err)
            }
            log.Debug("Sleeping ", Conf.WatchInterval)
            time.Sleep(Conf.WatchInterval)
        }
    },
}

func InitRootCmd() {
    RootCmd.AddCommand(VersionCmd)
    RootCmd.AddCommand(RunCmd)
    RootCmd.AddCommand(ConfSampleCmd)
}

// Main
func main() {
    InitRootCmd()

    if err := RootCmd.Execute(); err != nil {
        log.Fatal(err)
    }
}
