package main

import (
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1beta1 "k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/tools/clientcmd"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/pkg/fields"

	"gopkg.in/mgo.v2"

	"strconv"
	"strings"
	"errors"
	"flag"
	"time"
	"fmt"

	"../common"
)

var swk8sClientSet *kubernetes.Clientset

func swk8sPodDelete(podname string) error {
	var orphan bool = false
	var grace int64 = 0
	var err error

	podiface := swk8sClientSet.Pods(v1.NamespaceDefault)
	err = podiface.Delete(podname,
				&v1.DeleteOptions{
					GracePeriodSeconds: &grace,
					OrphanDependents: &orphan,
				})
	if err != nil {
		return fmt.Errorf("swk8sPodDelete: Can't delete %s: %s",
					podname, err.Error())
	}
	return nil
}

func swk8sSecretRemove(depname string) error {
	var orphan bool = false
	var grace int64 = 0
	var err error

	secrets := swk8sClientSet.Secrets(v1.NamespaceDefault)
	err = secrets.Delete(depname,
				&v1.DeleteOptions{
					GracePeriodSeconds: &grace,
					OrphanDependents: &orphan,
				})
	if err != nil {
		log.Errorf("Can't remove secret %s: %s",
				depname, err.Error())
		return err
	}

	log.Debugf("Removed secret %s", depname)
	return nil
}

func swk8sRemove(conf *YAMLConf, fn *FunctionDesc, fi *FnInst) error {
	var nr_replicas int32 = 0
	var orphan bool = false
	var grace int64 = 0
	var err error

	depname := fi.DepName()

	err = BalancerDelete(depname)
	if err != nil && err != mgo.ErrNotFound {
		log.Errorf("Can't delete balancer %s : %s", depname, err.Error())
		return err
	}

	deploy := swk8sClientSet.Extensions().Deployments(v1.NamespaceDefault)
	this, err := deploy.Get(depname)
	if err != nil {
		log.Errorf("Can't get deployment for %s", fn.FuncName)
		return err
	}

	this.Spec.Replicas = &nr_replicas
	_, err = deploy.Update(this)
	if err != nil {
		log.Errorf("Can't shrink replicas for %s: %s", fn.FuncName, err.Error())
		return err
	}

	err = deploy.Delete(depname,
				&v1.DeleteOptions{
					TypeMeta: unversioned.TypeMeta{
						Kind:       "Deployment",
						APIVersion: "extensions/v1beta1",
					},
					GracePeriodSeconds: &grace,
					OrphanDependents: &orphan,
				})
	if err != nil {
		log.Errorf("Can't delete deployment for %s: %s",
				fn.FuncName, err.Error())
		return err
	}

	swk8sSecretRemove(depname)

	log.Debugf("Deleted deployment for %s", fn.FuncName)
	return nil
}

func swk8sGenEnvVar(conf *YAMLConf, fn *FunctionDesc, fi *FnInst, wdaddr string, wd_port int32, secret *v1.Secret) []v1.EnvVar {
	var s []v1.EnvVar

	for _, v := range fn.Script.Env {
		vs := strings.SplitN(v, "=", 2)
		if strings.HasPrefix(vs[0], "SWD_") {
			// FIXME -- check earlier and abort adding
			log.Warn("Bogus env %s in %s.%s", vs[0], fn.Project, fn.FuncName)
			continue
		}
		s = append(s, v1.EnvVar{Name: vs[0], Value: vs[1]})
	}

	s = append(s,  v1.EnvVar{
			Name:	"SWD_FUNCTION_DESC",
			Value:	genFunctionDescJSON(conf, fn, fi), })

	if wdaddr != "" {
		s = append(s, v1.EnvVar{
				Name:	"SWD_ADDR",
				Value:	wdaddr, })
	}
	s = append(s, v1.EnvVar{
			Name:	"SWD_PORT",
			Value:	strconv.Itoa(int(wd_port)), })
	s = append(s, v1.EnvVar{
			Name:	"SWD_PROJECT",
			Value:	fn.Project, })
	s = append(s, v1.EnvVar{
			Name:	"SWD_FUNCNAME",
			Value:	fn.FuncName, })
	s = append(s, v1.EnvVar{
			Name: "SWD_COMMIT_ID",
			Value:	fn.Commit, })

	s = append(s, v1.EnvVar{
			Name:	"SWD_POD_IP",
			ValueFrom:
				&v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector {
						FieldPath: "status.podIP",
					},
				},
			})
	s = append(s, v1.EnvVar{
			Name:	"SWD_NODE_NAME",
			ValueFrom:
				&v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector {
						FieldPath: "spec.nodeName",
					},
				},
			})
	s = append(s, v1.EnvVar{
			Name:	"SWD_POD_NAME",
			ValueFrom:
				&v1.EnvVarSource{
					FieldRef: &v1.ObjectFieldSelector {
						FieldPath: "metadata.name",
					},
				},
			})

	for key, _ := range secret.Data {
		s = append(s, v1.EnvVar{
			Name:	key,
			ValueFrom:
				&v1.EnvVarSource{
					SecretKeyRef: &v1.SecretKeySelector {
						LocalObjectReference: v1.LocalObjectReference {
							Name: secret.ObjectMeta.Name,
						},
						Key: key,
					},
				},
			})
	}
	return s
}

func swk8sGenSecretData(conf *YAMLConf, fn *FunctionDesc) map[string][]byte {
	secret := make(map[string][]byte)

	mwarevars, err := mwareGetFnEnv(conf, fn)
	if err == nil && mwarevars != nil {
		for _, i := range mwarevars {
			j := strings.Split(i, ";")
			for _, k := range j {
				m := strings.Split(k, "=")
				if len(m) > 1 {
					secret[m[0]] = []byte(m[1])
				}
			}
		}
	}

	return secret
}

func swk8sGenLabels(depname string) map[string]string {
	labels := map[string]string {
		"deployment":	depname,
	}
	return labels
}

func swk8sSecretAdd(conf *YAMLConf, fn *FunctionDesc, depname string) (*v1.Secret, error) {
	secrets := swk8sClientSet.Secrets(v1.NamespaceDefault)
	secret, err := secrets.Create(&v1.Secret{
			ObjectMeta:	v1.ObjectMeta {
				Name:	depname,
				Labels:	swk8sGenLabels(depname),
			},
			Data:		swk8sGenSecretData(conf, fn),
			Type:		v1.SecretTypeOpaque,
		})
	if err != nil {
		err = fmt.Errorf("Can't create secret: %s", err.Error())
		log.Errorf("swk8sSecretSetup: %s", err.Error())
		return nil, err
	}

	return secret, nil
}

func swk8sUpdate(conf *YAMLConf, fn *FunctionDesc) error {
	// FIXME -- k8s has built-in update functionality. USE IT
	err := swk8sRemove(conf, fn, fn.InstOld())
	if err == nil {
		err = swk8sRun(conf, fn, fn.Inst())
	}

	return err
}

func swk8sRun(conf *YAMLConf, fn *FunctionDesc, fi *FnInst) error {
	var hostnw bool = false
	var err error

	var ctPorts []v1.ContainerPort

	depname := fi.DepName()

	rt, ok := conf.Runtime[fn.Script.Lang]
	if !ok {
		err := errors.New("Wrong language selected")
		log.Error("Wrong language")
		return err
	}

	secret, err := swk8sSecretAdd(conf, fn, depname)
	if err != nil {
		err := fmt.Errorf("Can't add a secret: %s", err.Error())
		log.Errorf("swk8sRun: %s", err.Error())
		return err
	}

	wdaddr := conf.Wdog.Addr
	wd_host, wd_port := swy.GetIPPort(conf.Wdog.Addr)
	if wd_host != "" {
		conf.Wdog.Addr = swy.MakeIPPort(wd_host, wd_port + 1)
		log.Debugf("conf.Wdog.Addr %s -> %s", wdaddr, conf.Wdog.Addr)
		hostnw = true
	} else {
		wdaddr = ""
	}

	if hostnw == true {
		ctPorts = append(ctPorts,
				v1.ContainerPort{
					ContainerPort: wd_port,
				})
	}

	podspec := v1.PodTemplateSpec{
		ObjectMeta:	v1.ObjectMeta {
			Name:	depname,
			Labels:	swk8sGenLabels(depname),
		},
		Spec:			v1.PodSpec {
			Volumes:	[]v1.Volume{
				{
					Name:		depname,
					VolumeSource:	v1.VolumeSource {
						HostPath: &v1.HostPathVolumeSource{
								Path: fnRepoCheckout(conf, fn),
							},
					},
				},
			},
			HostNetwork:	hostnw,
			Containers:	[]v1.Container{
				{
					Name:		fn.FuncName,
					Image:		rt.Image,
					Command:	[]string{conf.Wdog.CtPath},
					Ports:		ctPorts,
					Env:		swk8sGenEnvVar(conf, fn, fi, wdaddr, wd_port, secret),
					VolumeMounts:	[]v1.VolumeMount{
						{
							Name:		depname,
							ReadOnly:	false,
							MountPath:	RtGetWdogPath(fn),
						},
					},
					ImagePullPolicy: v1.PullNever,
				},
			},
		},
	}

	nr_replicas := fi.Replicas()

	err = BalancerCreate(depname, uint(nr_replicas))
	if err != nil {
		log.Errorf("Can't create balancer %s for %s/%s: %s",
				depname, fn.Project, fn.FuncName, err.Error())
		return err
	}

	deployspec := v1beta1.Deployment{
		TypeMeta: unversioned.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "extensions/v1beta1",
		},
		ObjectMeta:	v1.ObjectMeta {
			Name:	depname,
		},
		Spec: v1beta1.DeploymentSpec{
			Replicas: &nr_replicas,
			Template: podspec,
		},
	}

	deploy := swk8sClientSet.Extensions().Deployments(v1.NamespaceDefault)
	_, err = deploy.Create(&deployspec)
	if err != nil {
		swk8sSecretRemove(depname)
		BalancerDelete(depname)
		log.Errorf("Can't add function %s: %s",
				fn.FuncName, err.Error())
	} else {
		log.Debugf("Started deployment %s", depname)
	}

	return err
}

func genBalancerPod(pod *v1.Pod) (BalancerPod) {
	var r  BalancerPod = BalancerPod {
		DepName:	pod.ObjectMeta.Labels["deployment"],
		UID:		string(pod.ObjectMeta.UID),
		WdogAddr:	pod.Status.PodIP,
		State:		swy.DBPodStateNak,
	}

	for _, c := range pod.Spec.Containers {
		for _, v := range c.Env {
			if v.Name == "SWD_PROJECT" {
				r.Project = v.Value
			} else if v.Name == "SWD_FUNCNAME" {
				r.FuncName = v.Value
			} else if v.Name == "SWD_PORT" {
				if r.WdogAddr != "" {
					r.WdogAddr += ":" + v.Value
				}
			}
		}
	}

	for _, v := range pod.Status.Conditions {
		if v.Type == v1.PodScheduled {
			if v.Status == v1.ConditionTrue {
				if r.State != swy.DBPodStateRdy {
					r.State = swy.DBPodStateQue
				}
			}
		} else if v.Type == v1.PodReady {
			if v.Status == v1.ConditionTrue {
				r.State = swy.DBPodStateRdy
			}
		}
	}

	if pod.Status.Phase != v1.PodRunning {
		if pod.Status.Phase == v1.PodPending {
			r.State = swy.DBPodStateQue
		} else if pod.Status.Phase == v1.PodSucceeded {
			r.State = swy.DBPodStateTrm
		} else if pod.Status.Phase == v1.PodFailed {
			r.State = swy.DBPodStateTrm
		} else {
			r.State = swy.DBPodStateNak
		}
	}

	if r.WdogAddr == "" || r.UID == "" ||
		r.Project == "" || r.FuncName == "" ||
		r.DepName == "" {
		r.State = swy.DBPodStateNak
	}

	return r
}

func swk8sPodAdd(obj interface{}) {
	pod := obj.(*v1.Pod)
	log.Debugf("swk8sPodAdd: deployment %s (%v)",
			pod.ObjectMeta.Labels["deployment"],
			genBalancerPod(pod))
}

func swk8sPodDel(obj interface{}) {
	pod := obj.(*v1.Pod)
	log.Debugf("swk8sPodDel: deployment %s (%v)",
			pod.ObjectMeta.Labels["deployment"],
			genBalancerPod(pod))
}

func swk8sPodUpd(obj_old, obj_new interface{}) {
	var err error = nil

	pod_old := genBalancerPod(obj_old.(*v1.Pod))
	pod_new := genBalancerPod(obj_new.(*v1.Pod))

	if pod_old.State != swy.DBPodStateRdy {
		if pod_new.State == swy.DBPodStateRdy {
			err = BalancerPodAdd(pod_new.DepName, pod_new.UID, pod_new.WdogAddr)
			if err != nil {
				log.Errorf("swk8sPodUpd: Can't add pod %s/%s/%s: %s",
						pod_new.DepName, pod_new.UID,
						pod_new.WdogAddr, err.Error())
				return
			}
			log.Debugf("swk8sPodUpd: Pod added %s/%s/%s/%s",
					pod_new.DepName, pod_new.UID, pod_new.WdogAddr)
			notifyPodUpdate(&pod_new)
		}
	} else {
		if pod_new.State != swy.DBPodStateRdy {
			err = BalancerPodDel(pod_new.DepName, pod_new.UID)
			if err != nil  && err != mgo.ErrNotFound {
				log.Errorf("swk8sPodUpd: Can't delete pod %s/%s/%s: %s",
						pod_new.DepName, pod_new.UID,
						pod_new.WdogAddr, err.Error())
				return
			}
			log.Debugf("swk8sPodUpd: Pod deleted %s/%s/%s",
					pod_new.DepName, pod_new.UID, pod_new.WdogAddr)
			notifyPodUpdate(&pod_new)
		}
	}
}

func swk8sInit(conf *YAMLConf) error {
	kubeconfig := flag.String("kubeconfig", conf.Kuber.ConfigPath, "path to the kubeconfig file")
	flag.Parse()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		log.Error("BuildConfigFromFlags: %s", err.Error())
		return err
	}

	swk8sClientSet, err = kubernetes.NewForConfig(config)
	if err != nil {
		log.Error("NewForConfig: %s", err.Error())
		return err
	}

	watchlist := cache.NewListWatchFromClient(swk8sClientSet.Core().RESTClient(),
							"pods", v1.NamespaceDefault,
							fields.Everything())
	_, controller := cache.NewInformer(watchlist, &v1.Pod{},
						time.Second * 0,
						cache.ResourceEventHandlerFuncs{
							AddFunc:	swk8sPodAdd,
							DeleteFunc:	swk8sPodDel,
							UpdateFunc:	swk8sPodUpd,
						})
	stop := make(chan struct{})
	go controller.Run(stop)

	return nil
}
