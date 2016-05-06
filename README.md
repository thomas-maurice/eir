# Eir - Simple monitoring & healing software
## Introduction
Eir is a simple monitoring piece of software, named after the Norse goddess of healing.

The aim is reacting to a server's change of state, to eventually heal it in an
automated way.

It has initially been written to monitor the glorious [EESTEC](https://eestec.net)
server by posting updates on our Slack channel, and then it evolved a bit to be
able to perform actions.

## How it works
The basics of this software is that a server can be in several states
such as, sorted by severity:

* `UNKNOWN`
* `OK`
* `WARNING`
* `CRITICAL`

This state is calculated by aggregating the result of probes, and selecting the most
severe state of the list. According to that, actions can be triggered in order to improve
the state of the server.

## What are probes ?
Probes are scripts that will monitor the machine. The
only thing is that in order to make Eir react to it, probes have to write a StatusFile in
a folder set in Eir's configuration.

The format is fairly simple :
```
<STATUS>
Eventually some text to describe what happened
```

Where status is one of `UNKNOWN`, `OK`, `WARNING`, `CRITICAL`.

## Features
* Post updates on Slack whenever the state of the server changes
* Post JSON object to given URLs when the state of the server changes (webhooks)
* Perform actions when the state of the server (**or a probe**) changes
* Expose an HTTP interface so that you can just `curl` the server's status

## Examples
This is for example what you get by curling the HTTP interface:
```
curl localhost:7979 2>> /dev/null| jq .
{
  "Status": "CRITICAL",
  "Hostname": "ljosalfheim.maurice.fr",
  "Details": [
    {
      "Name": "nginx",
      "Status": "OK",
      "Text": "nginx up and running"
    },
    {
      "Name": "postfix",
      "Status": "CRITICAL",
      "Text": "postfix mail server is not running !"
    },
    {
      "Name": "root_filesystem",
      "Status": "WARNING",
      "Text": "78% of the rootfs is used"
    }
  ]
}
```

This is for instance what you get inside a webhook callback :
```
nc -l -p 8080
POST / HTTP/1.1
Host: localhost:8080
User-Agent: Go-http-client/1.1
Content-Length: 296
Accept-Encoding: gzip

{"Status":"CRITICAL","Hostname":"ljosalfheim.maurice.fr","Details":[{"Name":"nginx","Status":"OK","Text":"nginx up and running"},{"Name":"postfix","Status":"CRITICAL","Text":"postfix mail server is not running !"},{"Name":"root_filesystem","Status":"WARNING","Text":"78% of the rootfs is used"}]}
```

## How actions are handled
The actions are triggered whenever the status of the server changes, or whenever the
status of a probe changes, depending on your configuration. The commands you specify
have to execute within a certain timeout (default 30sec) or they will be killed.
The commands are run at the same time in separate goroutines.

If the configuration parameter DryRun is set to true, then the actions will not be
executed, they will just e printed.

## Sample configuration file
```yaml
# If you want updates on Slack, you have to provide both the token
# and the channel
SlackToken: "my awesome slack token"
SlackChannel: "#monitoring"
# Interval between two result directory checks
WatchInterval: 60
# Status file, so that Eir can be restarted when you want
StatusFile: ./status.yml
# Where the results of the probes are stored
ResultDir: results
# Do we run in debug mode ?
Debug: true
# List of URLs to notify about a change of state
WebHooks:
  - http://localhost:8080
# Timeout for those requests
WebHooksTimeout: 2
# Do we enable the HTTP interface ?
EnableHttpStatus: true
# Bind address
HttpListenOn: "0.0.0.0:8080"
# DryRun mode, if true, the actions will not be executed
DryRun: false
# Actions ! :D
Actions:
  # Actions that are run when the Global state of the server changes
  Global:
    OnOk:
      - Command: /bin/touch ./test.touch
        Timeout: 10
      - Command: sleep 40
      - Command: blarg
        Timeout: 4
  # Actions run when a particular probe changes state
  Probes:
    # Postfix for example
    postfix:
      OnCritical:
        - Command: systemctl restart postfix
      OnOk:
        - Command: echo "Yay !"
```

# License
```
           DO WHAT THE FUCK YOU WANT TO PUBLIC LICENSE
                   Version 2, December 2004

Copyright (C) 2016 Thomas Maurice <thomas@maurice.fr>

Everyone is permitted to copy and distribute verbatim or modified
copies of this license document, and changing it is allowed as long
as the name is changed.

           DO WHAT THE FUCK YOU WANT TO PUBLIC LICENSE
  TERMS AND CONDITIONS FOR COPYING, DISTRIBUTION AND MODIFICATION

 0. You just DO WHAT THE FUCK YOU WANT TO.
```

# Contributors
* Thomas Maurice ([@thomas_maurice](https://twitter.com/thomas_maurice))
