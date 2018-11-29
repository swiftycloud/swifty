/*
 * © 2018 SwiftyCloud OÜ. All rights reserved.
 * Info: info@swifty.cloud
 */

package main

import (
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/api/extensions/v1beta1"
	"k8s.io/client-go/tools/clientcmd"
	k8serr "k8s.io/apimachinery/pkg/api/errors"

	"k8s.io/client-go/tools/cache"
	"k8s.io/apimachinery/pkg/fields"

	"gopkg.in/mgo.v2/bson"

	"path/filepath"
	"context"
	"strconv"
	"strings"
	"errors"
	"flag"
	"time"
	"fmt"
	"net"
)

const wdogCresponderDir = "/var/run/swifty"

var k8sClientSet *kubernetes.Clientset

func k8sPodDelete(podname string) error {
	var orphan bool = false
	var grace int64 = 0
	var err error

	podiface := k8sClientSet.CoreV1().Pods(conf.Wdog.Namespace)
	err = podiface.Delete(podname,
				&metav1.DeleteOptions{
					GracePeriodSeconds: &grace,
					OrphanDependents: &orphan,
				})
	if err != nil {
		return fmt.Errorf("k8sPodDelete: Can't delete %s: %s",
					podname, err.Error())
	}
	return nil
}

func k8sRemove(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) error {
	var nr_replicas int32 = 0
	var orphan bool = false
	var grace int64 = 0
	var err error

	depname := fn.DepName()

	err = BalancerDelete(ctx, fn.Cookie)
	if err != nil {
		ctxlog(ctx).Errorf("Can't delete balancer %s : %s", depname, err.Error())
		return err
	}

	deploy := k8sClientSet.Extensions().Deployments(conf.Wdog.Namespace)
	this, err := deploy.Get(depname, metav1.GetOptions{})
	if err != nil {
		if k8serr.IsNotFound(err) {
			return nil
		}

		ctxlog(ctx).Errorf("Can't get deployment for %s", fn.SwoId.Str())
		return err
	}

	this.Spec.Replicas = &nr_replicas
	_, err = deploy.Update(this)
	if err != nil {
		ctxlog(ctx).Errorf("Can't shrink replicas for %s: %s", fn.SwoId.Str(), err.Error())
		return err
	}

	err = deploy.Delete(depname,
				&metav1.DeleteOptions{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Deployment",
						APIVersion: "extensions/v1beta1",
					},
					GracePeriodSeconds: &grace,
					OrphanDependents: &orphan,
				})
	if err != nil {
		ctxlog(ctx).Errorf("Can't delete deployment for %s: %s",
				fn.SwoId.Str(), err.Error())
		return err
	}

	ctxlog(ctx).Debugf("Remove %s deploy %s", fn.SwoId.Str(), depname)
	return nil
}

func k8sGenEnvVar(ctx context.Context, fn *FunctionDesc, wd_port int) []v1.EnvVar {
	var s []v1.EnvVar

	for _, v := range fn.Code.Env {
		vs := strings.SplitN(v, "=", 2)
		if len(vs) == 2 && vs[0] != "" {
			s = append(s, v1.EnvVar{Name: vs[0], Value: vs[1]})
		}
	}

	s = append(s, v1.EnvVar{
			Name:	"SWD_CRESPONDER",
			Value:	wdogCresponderDir, })
	s = append(s, v1.EnvVar{
			Name:	"SWD_LANG",
			Value:	fn.Code.Lang, })
	s = append(s, v1.EnvVar{
			Name:	"SWD_POD_TOKEN",
			Value:	fn.PodToken(), })
	s = append(s, v1.EnvVar{
			Name:	"SWD_FN_TMO",
			Value:	strconv.Itoa(int(fn.Size.Tmo)), })
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
			Name:	"SWD_VERSION",
			Value:	fn.Src.Version, })

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

	for _, mw := range fn.Mware {
		se, err := mwareGetEnvData(ctx, fn.SwoId, mw)
		if err != nil {
			ctxlog(ctx).Errorf("No mware %s for %s", mw, fn.SwoId.Str())
			continue
		}

		s = se.appendTo(s)
	}

	for _, aid := range fn.Accounts {
		se, err := accGetEnvData(ctx, fn.SwoId, aid)
		if err != nil {
			ctxlog(ctx).Errorf("No account %s for %s", aid, fn.SwoId.Str())
			continue
		}

		s = se.appendTo(s)
	}

	for _, s3b := range(fn.S3Buckets) {
		envs, err := s3GenBucketKeys(ctx, &fn.SwoId, s3b)
		if err != nil {
			ctxlog(ctx).Errorf("No s3 bucket secret for %s", s3b)
			continue
		}

		for en, ev := range(envs) {
			s = append(s, v1.EnvVar{ Name:en, Value:ev })
		}
	}

	return s
}

func k8sGenLabels(fn *FunctionDesc, depname string) map[string]string {
	labels := map[string]string {
		"deployment":	depname,
		"fnid":		fn.k8sId(),
	}
	return labels
}

func k8sUpdate(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) error {
	depname := fn.DepName()

	deploy := k8sClientSet.Extensions().Deployments(conf.Wdog.Namespace)
	this, err := deploy.Get(depname, metav1.GetOptions{})
	if err != nil {
		ctxlog(ctx).Errorf("Can't get deployment for %s", fn.SwoId.Str())
		return err
	}

	/*
	 * Function sources may be at the new location now
	 */
	for i := 0; i < len(this.Spec.Template.Spec.Volumes); i++ {
		vol := &this.Spec.Template.Spec.Volumes[i]
		if vol.Name == "code" {
			vol.VolumeSource.HostPath.Path = fn.srcPath("")
			break
		}
	}

	/*
	 * Tune up SWD_FUNCTION_DESC to make wdog keep up with
	 * updated Tmo value and MWARE_* secrets
	 */
	this.Spec.Template.Spec.Containers[0].Env = k8sGenEnvVar(ctx, fn, conf.Wdog.Port)

	specSetRes(&this.Spec.Template.Spec.Containers[0].Resources, fn)

	if fn.Size.Replicas == 1 {
		/* Don't let pods disappear at all */
		one := intstr.FromInt(1)
		zero := intstr.FromInt(0)

		this.Spec.Strategy = v1beta1.DeploymentStrategy {
			RollingUpdate: &v1beta1.RollingUpdateDeployment {
				MaxUnavailable: &zero,
				MaxSurge: &one,
			},
		}
	}

	_, err = deploy.Update(this)
	if err != nil {
		ctxlog(ctx).Errorf("Can't shrink replicas for %s: %s", fn.SwoId.Str(), err.Error())
		return err
	}

	return err
}

func specSetRes(res *v1.ResourceRequirements, fn *FunctionDesc) {
	mem_max := fmt.Sprintf("%dMi", fn.Size.Mem)
	mem_min := fmt.Sprintf("%dMi", fn.Size.Mem / 2)

	res.Limits = v1.ResourceList {
		v1.ResourceMemory:	resource.MustParse(mem_max),
	}
	res.Requests = v1.ResourceList {
		v1.ResourceMemory:	resource.MustParse(mem_min),
	}
}

func k8sRun(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) error {
	var err error
	roRoot := true

	depname := fn.DepName()
	ctxlog(ctx).Debugf("Start %s deploy for %s (img: %s)", fn.SwoId.Str(), depname, fn.Code.image())

	envs := k8sGenEnvVar(ctx, fn, conf.Wdog.Port)

	vols := []v1.Volume {
		{
			Name:		"code",
			VolumeSource:	v1.VolumeSource {
				HostPath: &v1.HostPathVolumeSource{
					Path: fn.srcPath(""),
				},
			},
		},
		{
			Name:		"conn",
			VolumeSource:	v1.VolumeSource {
				HostPath: &v1.HostPathVolumeSource{
					Path: "/var/run/swifty/wdogconn/" + fn.PodToken(),
				},
			},
		},
	}

	vols_m := []v1.VolumeMount {
		{
			Name:		"code",
			ReadOnly:	false,
			MountPath:	rtCodePath(&fn.Code),
		},
		{
			Name:		"conn",
			ReadOnly:	false,
			MountPath:	wdogCresponderDir,
		},
	}

	h, m, ok := rtPackages(fn.SwoId, fn.Code.Lang)
	if ok {
		vols = append(vols, v1.Volume {
			Name:		"pkg",
			VolumeSource:	v1.VolumeSource {
				HostPath: &v1.HostPathVolumeSource {
					Path: h,
				},
			},
		})

		vols_m = append(vols_m, v1.VolumeMount {
			Name:		"pkg",
			ReadOnly:	false,
			MountPath:	m,
		})
	}

	podspec := v1.PodTemplateSpec{
		ObjectMeta:	metav1.ObjectMeta {
			Name:	depname,
			Labels:	k8sGenLabels(fn, depname),
		},
		Spec:			v1.PodSpec {
			Volumes:	vols,
			HostNetwork:	false,
//			HostAliases:	[]v1.HostAlias {
//				FIXME -- resolve and add s3 API endpoint here
//				v1.HostAlias {
//					IP: "1.2.3.4",
//					Hostnames: []string{ conf.Mware.S3.API },
//				},
//			},
			Containers:	[]v1.Container{
				{
					Name:		"wdog",
					Image:		fn.Code.image(),
					Env:		envs,
					VolumeMounts:	vols_m,
					ImagePullPolicy: v1.PullNever,
					SecurityContext: &v1.SecurityContext {
						ReadOnlyRootFilesystem: &roRoot,
					},
				},
			},
		},
	}

	specSetRes(&podspec.Spec.Containers[0].Resources, fn)

	nr_replicas := int32(fn.Size.Replicas)

	err = BalancerCreate(ctx, fn.Cookie)
	if err != nil {
		ctxlog(ctx).Errorf("Can't create balancer %s for %s: %s", depname, fn.SwoId.Str(), err.Error())
		return errors.New("Net error")
	}

	deployspec := v1beta1.Deployment{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Deployment",
			APIVersion: "extensions/v1beta1",
		},
		ObjectMeta:	metav1.ObjectMeta {
			Name:	depname,
		},
		Spec: v1beta1.DeploymentSpec{
			Replicas: &nr_replicas,
			Template: podspec,
		},
	}

	deploy := k8sClientSet.Extensions().Deployments(conf.Wdog.Namespace)
	_, err = deploy.Create(&deployspec)
	if err != nil {
		BalancerDelete(ctx, fn.Cookie)
		ctxlog(ctx).Errorf("Can't start deployment %s: %s", fn.SwoId.Str(), err.Error())
		return errors.New("K8S error")
	}

	return nil
}

type k8sPod struct {
	SwoId
	FnId		string
	Token		string
	Version		string
	DepName		string
	WdogAddr	string
	WdogPort	string
	Host		string
	UID		string
}

func (pod *k8sPod)conn() *podConn {
	return &podConn {
		Addr: pod.WdogAddr,
		Port: pod.WdogPort,
		Host: pod.Host,
		FnId: pod.FnId,
		PTok: pod.Token,
	}
}

func (pod *k8sPod)Service() string {
	switch pod.DepName {
	case "swy-go-service", "swy-golang-service":
		return "golang"
	case "swy-swift-service":
		return "swift"
	case "swy-python-service":
		return "python"
	case "swy-ruby-service":
		return "ruby"
	case "swy-nodejs-service":
		return "nodejs"
	}

	return ""
}

func genBalancerPod(pod *v1.Pod) (*k8sPod) {
	r := &k8sPod {
		DepName:	pod.ObjectMeta.Labels["deployment"],
		UID:		string(pod.ObjectMeta.UID),
		WdogAddr:	pod.Status.PodIP,
		Host:		pod.Status.HostIP,
	}

	for _, c := range pod.Spec.Containers {
		for _, v := range c.Env {
			switch v.Name {
			case "SWD_TENNANT":
				r.Tennant = v.Value
			case "SWD_PROJECT":
				r.Project = v.Value
			case "SWD_FUNCNAME":
				r.Name = v.Value
			case "SWD_VERSION":
				r.Version = v.Value
			case "SWD_PORT":
				r.WdogPort = v.Value
			case "SWD_POD_TOKEN":
				r.Token = v.Value
			}
		}
	}

	r.FnId = r.SwoId.Cookie()

	return r
}

func k8sPodAdd(obj interface{}) {
}

func k8sPodDel(obj interface{}) {
	po := obj.(*v1.Pod)

/*
 *	poc := ""
 *	for _, cond := range po.Status.Conditions {
 *		poc += string(cond.Type) + "=" + string(cond.Status) + ","
 *	}
 *
 *	glog.Debugf("POD events\n%s:%s:%s:%s:%s ->DELETE\n",
 *		po.ObjectMeta.Labels["deployment"], po.ObjectMeta.UID, po.Status.PodIP, po.Status.Phase, poc)
 */

	if po.Status.PodIP != "" {
		podEvents <- &podEvent{up: false, pod: genBalancerPod(po)}
	}
}

func waitPodPort(ctx context.Context, addr, port string) error {
	printed := false
	lat := time.Duration(0)
	wt := PodStartBase
	till := time.Now().Add(PodStartTmo)
	for {
		conn, err := net.Dial("tcp", addr + ":" + port)
		if err == nil {
			wdogWaitLat.Observe(lat.Seconds())
			conn.Close()
			break
		}

		if time.Now().After(till) {
			wdogErrors.WithLabelValues("Start timeout").Inc()
			return fmt.Errorf("Pod's port not up for too long")
		}

		/*
		 * Kuber sends us POD-Up event when POD is up, not when
		 * watchdog is ready :) But we need to make sure that the
		 * port is open and ready to serve connetions. Possible
		 * solution might be to make wdog ping us after openeing
		 * its socket, but ... will gate stand that ping flood?
		 *
		 * Moreover, this port waiter is only needed when the fn
		 * is being waited for.
		 */
		if !printed {
			ctxlog(ctx).Debugf("Port %s:%s not open yet (%s) ... polling", addr, port, err.Error())
			printed = true
			portWaiters.Inc()
			defer portWaiters.Dec()
		}
		<-time.After(wt)
		lat += wt
		wt += PodStartGain
	}

	return nil
}

func k8sPodUp(ctx context.Context, pod *k8sPod) error {
	lng := pod.Service()
	if lng != "" {
		ctxlog(ctx).Debugf("Update %s service to %s", lng, pod.WdogAddr)
		rtSetService(lng, pod.WdogAddr)
		return nil
	}

	go func() {
		ctx, done := mkContext("::podwait")
		defer done(ctx)

		err := waitPodPort(ctx, pod.WdogAddr, pod.WdogPort)
		if err != nil {
			ctxlog(ctx).Errorf("POD %s port wait err: %s",
					pod.UID, err.Error())
			return
		}

		BalancerPodAdd(ctx, pod)
		notifyPodUp(ctx, pod)
	}()

	return nil
}

func k8sPodDown(ctx context.Context, pod *k8sPod) {
	if pod.Service() != "" {
		return
	}

	BalancerPodDel(ctx, pod)
	notifyPodDown(ctx, pod)
}

var showPodUpd bool

func init() {
	addBoolSysctl("pod_show_updates", &showPodUpd)
}

func k8sPodUpd(obj_old, obj_new interface{}) {
	po := obj_old.(*v1.Pod)
	pn := obj_new.(*v1.Pod)

	if showPodUpd {
		poc := ""
		pnc := ""
		for _, cond := range po.Status.Conditions {
			poc += string(cond.Type) + "=" + string(cond.Status) + ","
		}
		for _, cond := range pn.Status.Conditions {
			pnc += string(cond.Type) + "=" + string(cond.Status) + ","
		}

		glog.Debugf("POD UPDATE dep:%s uid:%s st:%s ph:%s co:%s -> st:%s ph:%s co:%s\n",
			po.ObjectMeta.Labels["deployment"], po.ObjectMeta.UID,
			po.Status.PodIP, po.Status.Phase, poc,
			pn.Status.PodIP, pn.Status.Phase, pnc)
	}


	if po.Status.PodIP == "" && pn.Status.PodIP != "" {
		podEvents <- &podEvent{up: true, pod: genBalancerPod(pn)}
	} else if po.Status.PodIP != "" && pn.Status.PodIP == "" {
		podEvents <- &podEvent{up: false, pod: genBalancerPod(pn)}
	} else if po.Status.PodIP != "" && pn.Status.PodIP != "" {
		if po.Status.PodIP != pn.Status.PodIP {
			glog.Errorf("BAD news: POD IP has changed, while shouldn't")
		}
	}
}

type podEvent struct {
	up	bool
	pod	*k8sPod
}

var podEvents chan *podEvent

func podEventLoop() {
	for {
		evt := <-podEvents

		ctx, done := mkContext("::podevent")
		tracePodEvent(ctx, evt)
		if evt.up {
			ctxlog(ctx).Debugf("POD %s (%s) up deploy %s",
				evt.pod.UID, evt.pod.WdogAddr, evt.pod.DepName)
			k8sPodUp(ctx, evt.pod)
		} else {
			ctxlog(ctx).Debugf("POD %s (%s) down deploy %s",
				evt.pod.UID, evt.pod.WdogAddr, evt.pod.DepName)
			k8sPodDown(ctx, evt.pod)
		}
		done(ctx)
	}
}

func init() {
	podEvents = make(chan *podEvent)
	go podEventLoop()
}

type secEnvs struct {
	id	string
	envs	map[string][]byte
}

func (se *secEnvs)appendTo(s []v1.EnvVar) []v1.EnvVar {
	for name, _ := range se.envs {
		s = append(s, v1.EnvVar { Name:	name,
			ValueFrom: &v1.EnvVarSource {
					SecretKeyRef: &v1.SecretKeySelector {
						LocalObjectReference: v1.LocalObjectReference {
							Name: se.id,
						},
						Key: name,
					},
				},
		})
	}

	return s
}

func (se *secEnvs)toK8Secret() *v1.Secret {
	return &v1.Secret{
		ObjectMeta:	metav1.ObjectMeta {
			Name:	se.id,
			Labels:	map[string]string{},
		},
		Data:		se.envs,
		Type:		v1.SecretTypeOpaque,
	}
}

func k8sSecretAdd(ctx context.Context, se *secEnvs) error {
	secrets := k8sClientSet.CoreV1().Secrets(conf.Wdog.Namespace)
	_, err := secrets.Create(se.toK8Secret())

	if err != nil {
		ctxlog(ctx).Errorf("secret add error: %s", err.Error())
		err = errors.New("K8S error")
	}

	return err
}

func k8sSecretMod(ctx context.Context, se *secEnvs) error {
	secrets := k8sClientSet.CoreV1().Secrets(conf.Wdog.Namespace)
	_, err := secrets.Update(se.toK8Secret())

	if err != nil {
		ctxlog(ctx).Errorf("secret update error: %s", err.Error())
		err = errors.New("K8S error")
	}

	return err
}

func k8sSecretRemove(ctx context.Context, id string) error {
	var orphan bool = false
	var grace int64 = 0
	var err error

	secrets := k8sClientSet.CoreV1().Secrets(conf.Wdog.Namespace)
	err = secrets.Delete(id,
		&metav1.DeleteOptions{
			GracePeriodSeconds: &grace,
			OrphanDependents: &orphan,
		})
	if err != nil {
		if k8serr.IsNotFound(err) {
			err = nil
		} else {
			ctxlog(ctx).Errorf("secret remove error: %s", id, err.Error())
		}
	}

	return err
}

func k8sDepScale(depname string, replicas int32, up bool) int32 {
	deps := k8sClientSet.Extensions().Deployments(conf.Wdog.Namespace)
	dep, err := deps.Get(depname, metav1.GetOptions{})
	if err != nil {
		return replicas /* Huh? */
	}

	if up {
		if *dep.Spec.Replicas >= replicas {
			return *dep.Spec.Replicas
		}
	} else {
		if *dep.Spec.Replicas <= replicas {
			return *dep.Spec.Replicas
		}
	}

	dep.Spec.Replicas = &replicas
	_, err = deps.Update(dep)
	if err != nil {
		return *dep.Spec.Replicas
	}

	return replicas
}

func k8sDepScaleUp(depname string, replicas uint32) uint32 {
	return uint32(k8sDepScale(depname, int32(replicas), true))
}

func k8sDepScaleDown(depname string, replicas uint32) uint32 {
	return uint32(k8sDepScale(depname, int32(replicas), false))
}

func k8sGetServicePods(ctx context.Context) (map[string]string, error) {
	rv := make(map[string]string)

	podiface := k8sClientSet.CoreV1().Pods(conf.Wdog.Namespace)
	pods, err := podiface.List(metav1.ListOptions{ LabelSelector: "swyservice" })
	if err != nil {
		ctxlog(ctx).Errorf("Error listing PODs: %s", err.Error())
		return nil, errors.New("Error listing PODs")
	}

	for _, pod := range pods.Items {
		lang := pod.ObjectMeta.Labels["swyservice"]
		rv[lang] = pod.Status.PodIP
	}

	return rv, nil
}

func ServiceDepsInit(ctx context.Context) error {
	srvIps, err := k8sGetServicePods(ctx)
	if err != nil {
		return err
	}

	for l, rt := range(rt_handlers) {
		if !rt.Disabled {
			ip, ok := srvIps[l]
			if !ok {
				if !rt.Build {
					continue
				}

				return fmt.Errorf("No builder for %s", l)
			}

			ctxlog(ctx).Debugf("Set %s as service for %s", ip, l)
			rt.ServiceIP= ip
		}
	}

	return nil
}

func listFnPods(fn *FunctionDesc) (*v1.PodList, error) {
	podiface := k8sClientSet.CoreV1().Pods(conf.Wdog.Namespace)
	return podiface.List(metav1.ListOptions{ LabelSelector: "fnid=" + fn.k8sId() })
}

func refreshDepsAndPods(ctx context.Context, hard bool) error {
	var fn FunctionDesc

	ctxlog(ctx).Debugf("Refreshing deps and pods (hard: %v)", hard)

	iter := dbIterAll(ctx, bson.M{}, &fn)
	defer iter.Close()

	depiface := k8sClientSet.Extensions().Deployments(conf.Wdog.Namespace)
	podiface := k8sClientSet.CoreV1().Pods(conf.Wdog.Namespace)

	for iter.Next(&fn) {
		if fn.State == DBFuncStateIni {
			/* Record reft from the early fn.Add stage. Just clean
			 * one out and forget the sources.
			 */
			err := removeSources(ctx, &fn)
			if err != nil {
				ctxlog(ctx).Errorf("Can't remove sources for %s: %s", fn.SwoId.Str(), err.Error())
				return err
			}

			err = dbRemove(ctx, fn)
			if err != nil {
				ctxlog(ctx).Errorf("db %s remove error: %s", fn.SwoId.Str(), err.Error())
				return err
			}

			continue
		}

		/* Try to restatr deps for starting fns and pick up the
		 * pods for ready ones. For hard-refresh, also restart
		 * the stalled ones
		 */
		if !(fn.State == DBFuncStateRdy || fn.State == DBFuncStateStr ||
				(fn.State == DBFuncStateStl && hard)) {
			continue
		}

		dep, err := depiface.Get(fn.DepName(), metav1.GetOptions{})
		if err != nil {
			if !k8serr.IsNotFound(err) {
				ctxlog(ctx).Errorf("Can't get dep %s: %s", fn.DepName(), err.Error())
				return errors.New("Error getting dep")
			}

			if fn.State == DBFuncStateStr || hard {
				/* That's OK, the deployment just didn't have time to
				 * to get created. Just create one and ... go agead,
				 * no replicas to check, no PODs to revitalize.
				 * For hard case, the function might have been marked
				 * as stalled for any reason, e.g. on restart due to dead
				 * k8s cluster. Anyway -- try to revitalize it.
				 */

				err = fn.Start(ctx)
				if err != nil {
					ctxlog(ctx).Errorf("Can't start back %s dep: %s", fn.SwoId.Str(), err.Error())
					return err
				}

				continue
			}

			if fn.State == DBFuncStateRdy {
				/* Function is running, but the deploy is not there
				 * Mark FN as stalled and let client handle it
				 */

				 fn.ToState(ctx, DBFuncStateStl, -1)
				 continue
			}

		}

		if *dep.Spec.Replicas > 1 {
			ctxlog(ctx).Debugf("Found grown-up (%d) deployment %s", *dep.Spec.Replicas, dep.Name)
			err = scalerInit(ctx, &fn, uint32(*dep.Spec.Replicas))
			if err != nil {
				ctxlog(ctx).Errorf("Can't reinit scaler: %s", err.Error())
				return err
			}
		}

		pods, err := podiface.List(metav1.ListOptions{ LabelSelector: "fnid=" + fn.k8sId() })
		if err != nil {
			ctxlog(ctx).Errorf("Error listing PODs: %s", err.Error())
			return errors.New("Error listing PODs")
		}

		for _, pod := range pods.Items {
			ctxlog(ctx).Debugf("Found pod %s %s", pod.Name, pod.Status.PodIP)
			err = k8sPodUp(ctx, genBalancerPod(&pod))
			if err != nil {
				ctxlog(ctx).Errorf("Can't refresh POD: %s", err.Error())
				return err
			}
		}
	}

	err := iter.Err()
	if err != nil {
		return err
	}

	return nil
}

func k8sInit(ctx context.Context, config_path string) error {
	config_path = filepath.Dir(config_path) + "/kubeconfig"
	kubeconfig := flag.String("kubeconfig", config_path, "path to the kubeconfig file")
	flag.Parse()

	if conf.Wdog.Namespace == "" {
		ctxlog(ctx).Debugf("Will work on %s k8s namespace", v1.NamespaceDefault)
		conf.Wdog.Namespace = v1.NamespaceDefault
	}

	addStringSysctl("k8s_namespace", &conf.Wdog.Namespace)

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		ctxlog(ctx).Errorf("BuildConfigFromFlags: %s", err.Error())
		return err
	}

	k8sClientSet, err = kubernetes.NewForConfig(config)
	if err != nil {
		ctxlog(ctx).Errorf("NewForConfig: %s", err.Error())
		return err
	}

	watchlist := cache.NewListWatchFromClient(k8sClientSet.Core().RESTClient(),
							"pods", conf.Wdog.Namespace,
							fields.Everything())
	_, controller := cache.NewInformer(watchlist, &v1.Pod{},
						time.Second * 0,
						cache.ResourceEventHandlerFuncs{
							AddFunc:	k8sPodAdd,
							DeleteFunc:	k8sPodDel,
							UpdateFunc:	k8sPodUpd,
						})
	stop := make(chan struct{})
	go controller.Run(stop)

	err = ServiceDepsInit(ctx)
	if err != nil {
		ctxlog(ctx).Errorf("Can't set up builder: %s", err.Error())
		return err
	}

	err = refreshDepsAndPods(ctx, false)
	if err != nil {
		ctxlog(ctx).Errorf("Can't sart scaler: %s", err.Error())
		return err
	}

	addSysctl("k8s_refresh", func() string { return "set soft/hard here" },
		func(v string) error {
			rctx, done := mkContext("::k8s-refresh")
			defer done(rctx)

			if v == "soft" {
				refreshDepsAndPods(rctx, false)
			}
			if v == "hard" {
				refreshDepsAndPods(rctx, true)
			}
			return nil
		},
	)

	return nil
}
