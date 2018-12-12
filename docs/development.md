
# Development

Things you might need to do.

## Set up types in kubernetes

```
kubectl apply -f examples/machine-controller.yaml -l local-testing="true"
```

## Run a local machine-controller

First, you need to make sure your kubernetes conf file is in place.

It needs to be .kubeconfig in the main directory.

```
hack/run-machine-controller.sh
```

if your DNS IP is not the default 172.16.0.10, run this way:

```
DNS_IP=10.96.0.10 hack/run-machine-controller.sh
```


