
write-script() {
  declare script="$1"
  echo "#!/bin/sh"
  echo "$script"
}

setup-daemon() {
  write-script "true" > /tmp/auth
  write-script "true" > /tmp/handler
  chmod +x /tmp/auth /tmp/handler
  sshfront -a /tmp/auth /tmp/handler &
  sleep 1
  expect <<-EOF
    set timeout 1
    spawn ssh test@localhost
    expect {
      timeout   { exit 1 }
      eof       { exit 1 }
      "Are you sure" {
        send "yes\r"
        sleep 1
        exit 0
      }
    }
    exit 1
EOF
  echo
}
setup-daemon

T_user-env() {
  write-script "env" > /tmp/handler
  expect <<-EOF
    set timeout 1
    spawn ssh foobar@localhost
    expect {
      timeout       { exit 1 }
      eof           { exit 1 }
      "USER=foobar" { exit 0 }
    }
    exit 1
EOF
}


T_echo-handler() {
  write-script 'echo $@' > /tmp/handler
  expect <<-EOF
    set timeout 1
    spawn ssh test@localhost foo bar baz
    expect {
      timeout       { exit 1 }
      eof           { exit 1 }
      "foo bar baz" { exit 0 }
    }
    exit 1
EOF
}

T_interactive-handler() {
  write-script 'exec bash' > /tmp/handler
  expect <<-EOF
    set timeout 1
    spawn ssh test@localhost
    expect {
      timeout       { exit 1 }
      eof           { exit 1 }
      "bash-4.3#" {
        send "echo hello\r"
        expect {
          timeout   { exit 1 }
          eof       { exit 1 }
          "hello"   { exit 0 }
        }
      }
    }
    exit 1
EOF
}
