build: execd
	go build .

example: build
	./execd -h localhost -p 2022 -k example/host_pk.pem example/authcheck example/helloworld
