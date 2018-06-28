## Istall this tools and libs on gw, mw, slave0, slave1

### install golang
```
dnf install golang
```

### configure working dir

```
mkdir -p /home/go
echo 'export GOPATH=/home/go' >> $HOME/.bashrc
source $HOME/.bashrc
```

### install libs
```
yum install librados2-devel
yum install glibc-headers
yum groupinstall "Development Libraries"
```

### clone swifty project
```
cd /home
git clone git@github.com:bbelky/swifty.git
```

### install deps
```
go get k8s.io/client-go/...
cd /home/go/src/k8s.io/client-go
git checkout -fb v2.0.0  v2.0.0
go get k8s.io/client-go/...
go get ./...
```

### build on gw
```
cd /home/swifty
make swifty/gate
make swifty/admd
```

## Execute this commands on appropriate hosts

### run docker containers on gw

```
docker stop swygate && docker rm swygate
docker stop swyadmd && docker rm swyadmd

docker run -d --net=host --name=swygate -v /etc/swifty:/etc/swifty -v /root/.swysecrets:/root/.swysecrets -v /etc/letsencrypt:/etc/letsencrypt swifty/gate
docker run -d --net=host --name=swyadmd -v /etc/swifty:/etc/swifty -v /root/.swysecrets:/root/.swysecrets -v /etc/letsencrypt:/etc/letsencrypt swifty/admd
```

### build on mw

```
cd /home/swifty
make swifty/s3
```

### run docker containers on mw

```
docker stop swys3 && docker rm swys3
docker run -d --net=host --name=swys3 -v /etc/swifty:/etc/swifty -v /root/.swysecrets:/root/.swysecrets -v /etc/letsencrypt:/etc/letsencrypt swifty/s3
```

### build on slaves

```
cd /home/swifty
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

# swifty
(С)SwiftyCloud OU, 2018
