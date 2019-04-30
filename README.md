## Swifty - serverless platform for application development

### What is Swifty?

Swifty is the serverless platform that allows startups, developers and enterprises to develop and run backend for applications and websites with minimal time-to-market, costs and without infrastructure management.
Swifty is available as a service here https://swifty.cloud and you can check how it works.

Swifty Dashboard is available here https://github.com/swiftycloud/swifty-dashboard

It is also available as an Open Source and you are free to use it for all our projects non- or commercial.

### You are welcome to contribute

Swifty is available with AGPL licenses and you are welcome to contribute and share any ideas you want to implement in a project.

## How to install Swifty using Ansible

Please use swifty-infrastructure guide here:
https://github.com/swiftycloud/swifty-infrastructure

## How to install Swifty on your server from source

### Requirements

You just need a single server to run Swifty backend and fronend. We recommend to use server with 4 vCPU and 8 GB of RAM at least. If you want to run many cuncurrent functions you obviously need to add more resources.

### clone swifty project
```
git clone https://github.com/swiftycloud/swifty
```

### configure GOPATH

```
cd swifty
echo 'export GOPATH=$(pwd)/vendor' >> $HOME/.bashrc
source $HOME/.bashrc
```


### install deps
```
make deps
```

## For gateway hosts

### build on gw
```
make swifty/gate
make swifty/admd
```

### run docker containers on gw

```
docker stop swygate && docker rm swygate
docker stop swyadmd && docker rm swyadmd

docker run -d --net=host --name=swygate -v /etc/swifty:/etc/swifty -v /root/.swysecrets:/root/.swysecrets -v /etc/letsencrypt:/etc/letsencrypt swifty/gate
docker run -d --net=host --name=swyadmd -v /etc/swifty:/etc/swifty -v /root/.swysecrets:/root/.swysecrets -v /etc/letsencrypt:/etc/letsencrypt swifty/admd
```

## For middleware hosts


### build on mw

```
make swifty/s3
```

### run docker containers on mw

```
docker stop swys3 && docker rm swys3
docker run -d --net=host --name=swys3 -v /etc/swifty:/etc/swifty -v /root/.swysecrets:/root/.swysecrets -v /etc/letsencrypt:/etc/letsencrypt swifty/s3
```

## For slave hosts


### build on slaves

```
make swifty/golang
make swifty/swift
```

### after previous step execute on gw

```
cd /home/swifty/kubectl/deploy
kubectl apply -f gobuild-dep.yaml (or for the first time kubectl create -f gobuild-dep.yaml)
kubectl apply -f swiftbuild-dep.yaml (or for the first time kubectl create -f swiftbuild-dep.yaml)
docker restart swygate
```
# Contact
mailto: vp@swifty.cloud
[Join slack](https://join.slack.com/t/swiftycloud/shared_invite/enQtNDk1Nzk5NTQ1OTIzLWVhNWY3ZDZmNmQ1YTBlZGNlN2IzMmNhYmEzNTNkOGU2MzdmZWE3YTBiMjVjYWI5Y2FhMTUwMWUyOTNkZGE5OTM)

# swifty
(С) SwiftyCloud OU, 2019
