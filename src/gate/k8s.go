package main

import (
	"k8s.io/client-go/pkg/api/resource"
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
		log.Errorf("Can't get deployment for %s", fn.SwoId.Str())
		return err
	}

	this.Spec.Replicas = &nr_replicas
	_, err = deploy.Update(this)
	if err != nil {
		log.Errorf("Can't shrink replicas for %s: %s", fn.SwoId.Str(), err.Error())
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
				fn.SwoId.Str(), err.Error())
		return err
	}

	swk8sSecretRemove(depname)

	log.Debugf("Deleted %s deployment %s", fn.SwoId.Str(), depname)
	return nil
}

func swk8sGenEnvVar(fn *FunctionDesc, fi *FnInst, wd_port int, secret *v1.Secret) []v1.EnvVar {
	var s []v1.EnvVar

	for _, v := range fn.Code.Env {
		vs := strings.SplitN(v, "=", 2)
		if strings.HasPrefix(vs[0], "SWD_") {
			// FIXME -- check earlier and abort adding
			log.Warn("Bogus env %s in %s", vs[0], fn.SwoId.Str())
			continue
		}
		s = append(s, v1.EnvVar{Name: vs[0], Value: vs[1]})
	}

	s = append(s,  v1.EnvVar{
			Name:	"SWD_FUNCTION_DESC",
			Value:	genFunctionDescJSON(fn, fi), })

	s = append(s, v1.EnvVar{
			Name:	"SWD_PORT",
			Value:	strconv.Itoa(int(wd_port)), })
	s = append(s, v1.EnvVar{
			Name:	"SWD_TENNANT",
			Value:	fn.Tennant, })
	s = append(s, v1.EnvVar{
			Name:	"SWD_PROJECT",
			Value:	fn.Project, })
	s = append(s, v1.EnvVar{
			Name:	"SWD_FUNCNAME",
			Value:	fn.Name, })

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
	var err error
	var mwarevars [][2]string
	secret := make(map[string][]byte)

	mwarevars, err = mwareGetFnEnv(conf, fn)
	if err == nil {
		for _, v := range mwarevars {
			secret[v[0]] = []byte(v[1])
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
	depname := fn.Inst().DepName()

	deploy := swk8sClientSet.Extensions().Deployments(v1.NamespaceDefault)
	this, err := deploy.Get(depname)
	if err != nil {
		log.Errorf("Can't get deployment for %s", fn.SwoId.Str())
		return err
	}

	/*
	 * Function sources are at the new location now, so we only
	 * need to fix that path and rollout the new version of the
	 * deployment.
	 */
	for i := 0; i < 2; i++ {
		if this.Spec.Template.Spec.Volumes[i].Name == "code" {
			this.Spec.Template.Spec.Volumes[i].VolumeSource.HostPath.Path = fnRepoCheckout(conf, fn)
			break;
		}
	}

	_, err = deploy.Update(this)
	if err != nil {
		log.Errorf("Can't shrink replicas for %s: %s", fn.SwoId.Str(), err.Error())
		return err
	}

	/*
	 * FIXME -- after the new version of the deployment is rolled
	 * out we may remove old checkout out sources
	 */

	return err
}

func swk8sRun(conf *YAMLConf, fn *FunctionDesc, fi *FnInst) error {
	var err error

	depname := fi.DepName()
	log.Debugf("Start %s deployment for %s", depname, fn.SwoId.Str())

	img, ok := conf.Runtime.Images[fn.Code.Lang]
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

	mem_max := fmt.Sprintf("%dMi", fn.Size.Mem)
	mem_min := fmt.Sprintf("%dMi", fn.Size.Mem / 2)

	// FIXME: Obtain them from settings and
	// account in backend database
	resspec := v1.ResourceRequirements {
		Limits: v1.ResourceList {
			v1.ResourceMemory:	resource.MustParse(mem_max),
		},
		Requests: v1.ResourceList {
			v1.ResourceMemory:	resource.MustParse(mem_min),
		},
	}

	envs := swk8sGenEnvVar(fn, fi, conf.Wdog.Port, secret)

	podspec := v1.PodTemplateSpec{
		ObjectMeta:	v1.ObjectMeta {
			Name:	depname,
			Labels:	swk8sGenLabels(depname),
		},
		Spec:			v1.PodSpec {
			Volumes:	[]v1.Volume{
				{
					Name:		"code",
					VolumeSource:	v1.VolumeSource {
						HostPath: &v1.HostPathVolumeSource{
								Path: fnRepoCheckout(conf, fn),
							},
					},
				},
				{
					Name:		"stats",
					VolumeSource:	v1.VolumeSource {
						HostPath: &v1.HostPathVolumeSource{
								Path: fnStatsDir(conf, fn),
							},
					},
				},
			},
			HostNetwork:	false,
			Containers:	[]v1.Container{
				{
					Name:		fn.Name,
					Image:		img,
					Env:		envs,
					VolumeMounts:	[]v1.VolumeMount{
						{
							Name:		"code",
							ReadOnly:	false,
							MountPath:	RtCodePath(&fn.Code),
						},
						{
							Name:		"stats",
							ReadOnly:	false,
							MountPath:	statsPodPath,
						},
					},
					ImagePullPolicy: v1.PullNever,
					Resources:	resspec,
				},
			},
		},
	}

	nr_replicas := fi.Replicas()

	err = BalancerCreate(depname, uint(nr_replicas))
	if err != nil {
		log.Errorf("Can't create balancer %s for %s: %s",
				depname, fn.SwoId.Str(), err.Error())
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
				fn.SwoId.Str(), err.Error())
	}

	return err
}

var podStates = map[int]string {
	swy.DBPodStateNak: "Nak",
	swy.DBPodStateQue: "Que",
	swy.DBPodStateRdy: "Rdy",
	swy.DBPodStateTrm: "Trm",
	swy.DBPodStateBsy: "Bsy",
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
			if v.Name == "SWD_TENNANT" {
				r.Tennant = v.Value
			} else if v.Name == "SWD_PROJECT" {
				r.Project = v.Value
			} else if v.Name == "SWD_FUNCNAME" {
				r.Name = v.Value
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
		r.Tennant == "" || r.Project == "" || r.Name == "" ||
		r.DepName == "" {
		r.State = swy.DBPodStateNak
	}

	return r
}

func swk8sPodAdd(obj interface{}) {
}

func swk8sPodDel(obj interface{}) {
}

func swk8sPodUpd(obj_old, obj_new interface{}) {
	var err error = nil

	pod_old := genBalancerPod(obj_old.(*v1.Pod))
	pod_new := genBalancerPod(obj_new.(*v1.Pod))

	if pod_old.State != swy.DBPodStateRdy {
		if pod_new.State == swy.DBPodStateRdy {
			log.Debugf("POD %s (%s) up (%s->%s) deploy %s", pod_new.UID, pod_new.WdogAddr,
					podStates[pod_old.State], podStates[pod_new.State],
					pod_new.DepName)

			err = BalancerPodAdd(pod_new.DepName, pod_new.UID, pod_new.WdogAddr)
			if err != nil {
				log.Errorf("Can't add pod %s/%s/%s: %s",
						pod_new.DepName, pod_new.UID,
						pod_new.WdogAddr, err.Error())
				return
			}
			notifyPodUpdate(&pod_new)
		}
	} else {
		if pod_new.State != swy.DBPodStateRdy {
			log.Debugf("POD %s down (%s->%s) deploy %s", pod_new.UID,
					podStates[pod_old.State], podStates[pod_new.State],
					pod_new.DepName)

			err = BalancerPodDel(pod_new.DepName, pod_new.UID)
			if err != nil  && err != mgo.ErrNotFound {
				log.Errorf("Can't delete pod %s/%s/%s: %s",
						pod_new.DepName, pod_new.UID,
						pod_new.WdogAddr, err.Error())
				return
			}
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
