FROM alpine
RUN apk --update add expect bash openssh curl \
  && ssh-keygen -t rsa -N "" -f /root/.ssh/id_rsa \
  && curl -Ls https://github.com/progrium/basht/releases/download/v0.1.0/basht_0.1.0_Linux_x86_64.tgz \
    | tar -zxC /bin
