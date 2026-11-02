// Package service runs the server at login, so it comes back after a crash: as a
// launchd agent on macOS and a scheduled task on Windows. Both live in the user's
// session rather than the system's, because pressing keys only works there.
package service

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"
	"unicode/utf16"
)

// Label names the agent to launchd and the task to Windows. It's also the
// identifier the binary is signed with.
const Label = "app.voistic.server"

const defaultPath = "/usr/local/bin:/usr/bin:/bin:/usr/sbin:/sbin"

// Plist returns the agent's definition. launchd passes no user environment, so
// PATH is spelled out for the actions in the config.
func Plist(executable, log string) ([]byte, error) {
	var out bytes.Buffer
	escape := func(s string) string {
		var escaped bytes.Buffer
		xml.EscapeText(&escaped, []byte(s))
		return escaped.String()
	}
	fmt.Fprintf(&out, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>%s</string>
  <key>ProgramArguments</key>
  <array>
    <string>%s</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Interactive</string>
  <key>EnvironmentVariables</key>
  <dict>
    <key>PATH</key>
    <string>%s</string>
  </dict>
  <key>StandardOutPath</key>
  <string>%s</string>
  <key>StandardErrorPath</key>
  <string>%s</string>
</dict>
</plist>
`, Label, escape(executable), defaultPath, escape(log), escape(log))
	return out.Bytes(), nil
}

// Task returns a Task Scheduler definition that runs the server at the user's
// login and restarts it after a crash. schtasks reads it as UTF-16.
func Task(executable, log, user string) []byte {
	escape := func(s string) string {
		var escaped bytes.Buffer
		xml.EscapeText(&escaped, []byte(s))
		return escaped.String()
	}
	arguments := fmt.Sprintf(`serve --log "%s"`, log)
	definition := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-16"?>
<Task version="1.2" xmlns="http://schemas.microsoft.com/windows/2004/02/mit/task">
  <RegistrationInfo>
    <Description>voistic server</Description>
  </RegistrationInfo>
  <Triggers>
    <LogonTrigger>
      <Enabled>true</Enabled>
      <UserId>%[1]s</UserId>
    </LogonTrigger>
  </Triggers>
  <Principals>
    <Principal id="Author">
      <UserId>%[1]s</UserId>
      <LogonType>InteractiveToken</LogonType>
      <RunLevel>LeastPrivilege</RunLevel>
    </Principal>
  </Principals>
  <Settings>
    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>
    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>
    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>
    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>
    <RestartOnFailure>
      <Interval>PT1M</Interval>
      <Count>999</Count>
    </RestartOnFailure>
  </Settings>
  <Actions Context="Author">
    <Exec>
      <Command>%[2]s</Command>
      <Arguments>%[3]s</Arguments>
    </Exec>
  </Actions>
</Task>
`, escape(user), escape(executable), escape(arguments))

	units := utf16.Encode([]rune(strings.ReplaceAll(definition, "\n", "\r\n")))
	out := []byte{0xFF, 0xFE} // the byte order mark
	for _, unit := range units {
		out = append(out, byte(unit), byte(unit>>8))
	}
	return out
}
