FROM golang

COPY mgvm.tar.gz /root/
RUN cd /root && tar xvf mgvm.tar.gz

# for basic editing inside the docker
RUN apt update
RUN apt install -y vim

WORKDIR /root

