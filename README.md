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

### build project
```
cd /home/swifty
make all
```


# swifty
(С)SwiftyCloud OU, 2018
