# Specifies a parent image
FROM docker:dind

# create a user with sudo access
RUN set -ex && apk --no-cache add sudo
RUN apk --no-cache add curl

# installing minikube
RUN curl -LO https://storage.googleapis.com/minikube/releases/latest/minikube-linux-amd64
RUN sudo install minikube-linux-amd64 /usr/local/bin/minikube && rm minikube-linux-amd64

# installing kubectl
RUN curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"

RUN sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl

# install go
COPY --from=golang:1.20.13-alpine /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

CMD /bin/bash

# Creates an app directory to hold your app’s source code
WORKDIR /app
 
# Copies everything from your root directory into /app
COPY . .
 
# Installs Go dependencies
RUN go mod download
 
# Builds your app with optional configuration
RUN go build ./test/utils/tutorials/tester.go
 
# Specifies the executable command that runs when the container starts
CMD [ "./tester" ]
