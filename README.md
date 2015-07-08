# sshfront

[![CircleCI](https://img.shields.io/circleci/project/gliderlabs/sshfront/release.svg)](https://circleci.com/gh/gliderlabs/sshfront)
[![IRC Channel](https://img.shields.io/badge/irc-%23gliderlabs-blue.svg)](https://kiwiirc.com/client/irc.freenode.net/#gliderlabs)

A lightweight SSH server frontend where authentication and connections
are controlled with command handlers / shell scripts.

## Using sshfront
```
Usage: ./sshfront [options] <handler>

  -a="": authentication hook. empty=allow all
  -d=false: debug mode
  -e=false: pass environment to handler
  -h="0.0.0.0": ip to listen on
  -k="~/.ssh/id_rsa": private host key path
  -p="22": port to listen on
```


#### handler $command...

 * `$command...` command line arguments specified to run by the SSH client

The handler is a command that's used to handle all SSH connections. Output, stderr, and the exit code is returned to the client. If the client provides stdin, that's passed to the handler.

If the authentication hook was specified, any output is parsed as environment variables and added to the handler environment. `$USER` is always the SSH user used to connect and `$SSH_ORIGINAL_COMMAND` is the command specified from the client if not interactive.

#### auth-hook $user $key

 * `$user` argument is the name of the user being used to attempt the connection
 * `$key` argument is the public key data being provided for authentication

The auth hook is a command used for authenticating incoming SSH connections. If it returns with exit status 0, the connection will be allowed, otherwise it will be denied. The output of auth hook must be empty, or key-value pairs in the form `KEY=value` separated by newlines, which will be added to the environment of connection handler.

The auth hook is optional, but if not specified then all connections are allowed.
It is a good idea to always specify an auth hook.

## Examples

**Many of these bypass authentication and may allow remote execution, *do not* run this in production.**

Echo server:

```
server$ sshfront $(which echo)
client$ ssh $SERVER "hello world"
hello world
```

Echo host's environment to clients:

```
server$ sshfront -e $(env)
client$ ssh $SERVER
USER=root
HOME=/root
LANG=en_US.UTF-8
...
```

Bash server:

```
server$ sshfront $(which bash)
client$ ssh $SERVER
bash-4.3$ echo "this is a bash instance running on the server"
this is a bash instance running on the server
```


## Sponsors

This project was made possible thanks to [Deis](http://deis.io) and [DigitalOcean](http://digitalocean.com).

## License

MIT
