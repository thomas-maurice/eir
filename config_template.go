package main

var ConfigSample = `# Eir sample configuration file, feel free to adapt !

# If you want updates on Slack, you have to provide both the token
# and the channel
SlackToken: "my awesome slack token"
SlackChannel: "#monitoring"
# Interval between two result directory checks
WatchInterval: 60
# Status file, so that Eir can be restarted when you want
StatusFile: ./status.yml
# Where the results of the probes are stored
ResultDir: results
# Do we run in debug mode ?
Debug: true
# List of URLs to notify about a change of state
WebHooks:
  - http://localhost:8080
# Timeout for those requests
WebHooksTimeout: 2
# Do we enable the HTTP interface ?
EnableHttpStatus: true
# Bind address
HttpListenOn: "0.0.0.0:8080"
# DryRun mode, if true, the actions will not be executed
DryRun: false
# Actions ! :D
Actions:
  # Actions that are run when the Global state of the server changes
  Global:
    OnOk:
      - Command: /bin/touch statechanged
        Timeout: 10
  # Actions run when a particular probe changes state
  Probes:
    # Postfix for example
    postfix:
      OnCritical:
        - Command: systemctl restart postfix
      OnOk:
        - Command: echo "Yay !"`
