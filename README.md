
## Common for all environments

### clone swifty project
```
git clone git@github.com:bbelky/swifty.git
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

# swifty
(С)SwiftyCloud OU, 2018
