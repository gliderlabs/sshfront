# sshfront

A lightweight SSH server frontend written in Go. Authentication and connections
are controlled with command handlers / shell scripts.

## Using sshfront
```
Usage: ./sshfront [options] <handler>

  -d=false: debug mode displays handler output
  -e=false: pass environment to handlers
  -k="": pem file of private keys (read from SSH_PRIVATE_KEYS by default)
  -h="": host ip to listen on
  -p="22": port to listen on
```


#### auth-handler $user $key

 * `$user` argument is the name of the user being used to attempt the connection
 * `$key` argument is the public key data being provided for authentication

auth-handler is the path to an executable that's used for authenticating incoming SSH connections. If it returns with exit status 0, the connection will be allowed, otherwise it will be denied. The output of auth-handler must be empty, or key-value pairs in the form `KEY=value` separated by newlines, which will be added to the environment of exec-handler.

Although auth-handler is required, you can still achieve no-auth open access by providing `/usr/bin/true` as auth-handler.


#### exec-handler $command...

 * `$command...` arguments is the command line that was specified to run by the SSH client

exec-handler is the path to an executable that's used to execute the command provided by the client. The meaning of that is quite flexible. All of the stdout and stderr is returned to the client, including the exit status. If the client provides stdin, that's passed to the exec-handler. Any environment variables provided by the auth-handler output will be available to exec-handler, as well as `$USER` and `$SSH_ORIGINAL_COMMAND` environment variables.


## Examples

**These examples bypass all authentication and allow remote execution, *do not* run this in production.**

Echo server (with accept-all auth):

```
server$ sshfront $(which true) $(which echo)
client$ ssh $SERVER "hello world"
hello world
```

Echo host's environment to clients (with accept-all auth):

```
server$ sshfront -e $(which true) $(env)
client$ ssh $SERVER
USER=root
HOME=/root
LANG=en_US.UTF-8
...
```

Bash server (with accept-all auth):

```
server$ sshfront $(which true) $(which bash)
client$ ssh $SERVER
bash-4.3$ echo "this is a bash instance running on the server"
this is a bash instance running on the server
```


## Sponsors

This project was made possible thanks to [DigitalOcean](http://digitalocean.com).

## License

BSD
