# execd

A very lightweight SSH server frontend written in Go. The backend auth and execution logic is handled by commands you specify, letting you customize its behavior via your own scripts/executables.

## Using execd
```
Usage: ./execd [options] <auth-handler> <exec-handler>

  -d=false: debug mode displays handler output
  -e=false: pass environment to handlers
  -k="": pem file of private keys (read from SSH_PRIVATE_KEYS by default)
  -p="22": port to listen on
  -s=false: run exec handler via SHELL
```
#### auth-handler $user $key

 * `$user` argument is the name of the user being used to attempt the connection
 * `$key` argument is the public key data being provided for authentication

auth-handler is the path to an executable that's used for authenticating incoming SSH connections. If it returns with exit status 0, the connection will be allowed, otherwise it will be denied. The output of auth-handler must be empty, or key-value pairs in the form `KEY=value` separated by newlines, which will be added to the environment of exec-handler.

Although auth-handler is required, you can still achieve no-auth open access by providing `/usr/bin/true` as auth-handler.

#### exec-handler $command...

 * `$command...` arguments is the command line that was specified to run by the SSH client

exec-handler is the path to an executable that's used to execute the command provided by the client. The meaning of that is quite flexible. All of the stdout and stderr is returned to the client, including the exit status. If the client provides stdin, that's passed to the exec-handler. Any environment variables provided by the auth-handler output will be available to exec-handler, as well as `$USER` and `$SSH_ORIGINAL_COMMAND` environment variables.

## Credit / History

It started with [gitreceive](https://github.com/progrium/gitreceive), which was then used in [Dokku](https://github.com/progrium/dokku). Then I made a more generalized version of gitreceive, more similar to execd, called [sshcommand](https://github.com/progrium/sshcommand), which eventually replaced gitreceive in Dokku. When I started work on Flynn, the first projects included [gitreceived](https://github.com/flynn/gitreceived) (a standalone daemon version of gitreceive). This was refined by the Flynn community, namely Jonathan Rudenberg. 

Eventually I came to realize gitreceived could be generalized / simplified further in a way that could be used *with* the original gitreceive, *and* replace sshcommand, *and* be used in Dokku, *and* potentially replace gitreceived in Flynn. This project takes learnings from all those projects, though mostly gitreceived.

## Sponsors

This project was made possible thanks to [DigitalOcean](http://digitalocean.com).

## License

BSD