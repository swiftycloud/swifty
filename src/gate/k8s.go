package main

import (
	"k8s.io/client-go/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/pkg/api/unversioned"
	"k8s.io/client-go/pkg/api/v1"
	"k8s.io/client-go/pkg/util/intstr"
	//metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1beta1 "k8s.io/client-go/pkg/apis/extensions/v1beta1"
	"k8s.io/client-go/tools/clientcmd"
	k8serr "k8s.io/client-go/pkg/api/errors"

	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/pkg/fields"

	"gopkg.in/mgo.v2/bson"

	"../common"
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

var swk8sClientSet *kubernetes.Clientset

func swk8sPodDelete(podname string) error {
	var orphan bool = false
	var grace int64 = 0
	var err error

	podiface := swk8sClientSet.Pods(conf.Wdog.Namespace)
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

func swk8sRemove(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) error {
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

	deploy := swk8sClientSet.Extensions().Deployments(conf.Wdog.Namespace)
	this, err := deploy.Get(depname)
	if err != nil {
		if k8serr.IsNotFound(err) {
			ctxlog(ctx).Debugf("Deployment %s/%s doesn't exist", fn.SwoId.Str(), depname)
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
				&v1.DeleteOptions{
					TypeMeta: unversioned.TypeMeta{
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

	ctxlog(ctx).Debugf("Deleted %s deployment %s", fn.SwoId.Str(), depname)
	return nil
}

func swk8sGenEnvVar(ctx context.Context, fn *FunctionDesc, wd_port int) []v1.EnvVar {
	var s []v1.EnvVar

	for _, v := range fn.Code.Env {
		vs := strings.SplitN(v, "=", 2)
		s = append(s, v1.EnvVar{Name: vs[0], Value: vs[1]})
	}

	s = append(s, v1.EnvVar{
			Name:	"SWD_LANG",
			Value:	fn.Code.Lang, })
	s = append(s, v1.EnvVar{
			Name:	"SWD_POD_TOKEN",
			Value:	fn.Cookie, })
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
		mwc, err := mwareGetCookie(ctx, fn.SwoId, mw)
		if err != nil {
			ctxlog(ctx).Errorf("No mware %s for %s", mw, fn.SwoId.Str())
			continue
		}

		secret, err := swk8sClientSet.Secrets(conf.Wdog.Namespace).Get("mw-" + mwc)
		if err != nil {
			ctxlog(ctx).Errorf("No mware secret for %s", mwc)
			continue
		}

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
	}

	for _, aid := range fn.Accounts {
		var ac AccDesc

		err := dbFind(ctx, bson.M{"_id": bson.ObjectIdHex(aid)}, &ac)
		if err != nil {
			ctxlog(ctx).Errorf("No account for %s", aid)
			continue
		}

		envs := ac.getEnv()
		for en, ev := range(envs) {
			s = append(s, v1.EnvVar{ Name:en, Value:ev })
		}
	}

	for _, s3b := range(fn.S3Buckets) {
		envs, err := GenBucketKeysS3(ctx, &conf.Mware, &fn.SwoId, s3b)
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

func swk8sGenLabels(fn *FunctionDesc, depname string) map[string]string {
	labels := map[string]string {
		"deployment":	depname,
		"swyrun":	fn.Cookie[:32],
	}
	return labels
}

func swk8sUpdate(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) error {
	depname := fn.DepName()

	deploy := swk8sClientSet.Extensions().Deployments(conf.Wdog.Namespace)
	this, err := deploy.Get(depname)
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
			vol.VolumeSource.HostPath.Path = fnCodeLatestPath(conf, fn)
			break
		}
	}

	/*
	 * Tune up SWD_FUNCTION_DESC to make wdog keep up with
	 * updated Tmo value and MWARE_* secrets
	 */
	this.Spec.Template.Spec.Containers[0].Env = swk8sGenEnvVar(ctx, fn, conf.Wdog.Port)

	specSetRes(&this.Spec.Template.Spec.Containers[0].Resources, fn)

	if fn.Size.Replicas == 1 {
		/* Don't let pods disappear at all */
		ctxlog(ctx).Debugf("Tuning up update strategy")
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

func swk8sRun(ctx context.Context, conf *YAMLConf, fn *FunctionDesc) error {
	var err error
	roRoot := true

	depname := fn.DepName()
	ctxlog(ctx).Debugf("Start %s deployment for %s (img: %s)", depname, fn.SwoId.Str(), "swifty/" + fn.Code.Lang)

	envs := swk8sGenEnvVar(ctx, fn, conf.Wdog.Port)

	podspec := v1.PodTemplateSpec{
		ObjectMeta:	v1.ObjectMeta {
			Name:	depname,
			Labels:	swk8sGenLabels(fn, depname),
		},
		Spec:			v1.PodSpec {
			Volumes:	[]v1.Volume{
				{
					Name:		"code",
					VolumeSource:	v1.VolumeSource {
						HostPath: &v1.HostPathVolumeSource{
								Path: fnCodeLatestPath(conf, fn),
							},
					},
				},
				{
					Name:		"conn",
					VolumeSource:	v1.VolumeSource {
						HostPath: &v1.HostPathVolumeSource{
								Path: "/var/run/swifty/wdogconn/" + fn.Cookie,
							},
					},
				},
			},
			HostNetwork:	false,
			Containers:	[]v1.Container{
				{
					Name:		"wdog",
					Image:		fn.Code.image(),
					Env:		envs,
					VolumeMounts:	[]v1.VolumeMount{
						{
							Name:		"code",
							ReadOnly:	false,
							MountPath:	RtCodePath(&fn.Code),
						},
						{
							Name:		"conn",
							ReadOnly:	false,
							MountPath:	"/var/run/swifty",
						},
					},
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

	deploy := swk8sClientSet.Extensions().Deployments(conf.Wdog.Namespace)
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
	Version		string
	DepName		string
	WdogAddr	string
	WdogPort	string
	Host		string
	UID		string
}

func (pod *k8sPod)Build() string {
	if pod.DepName == "swy-go-builder" {
		return "golang"
	}
	if pod.DepName == "swy-swift-builder" {
		return "swift"
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
			if v.Name == "SWD_TENNANT" {
				r.Tennant = v.Value
			} else if v.Name == "SWD_PROJECT" {
				r.Project = v.Value
			} else if v.Name == "SWD_FUNCNAME" {
				r.Name = v.Value
			} else if v.Name == "SWD_VERSION" {
				r.Version = v.Value
			} else if v.Name == "SWD_PORT" {
				r.WdogPort = v.Value
			}
		}
	}

	return r
}

func swk8sPodAdd(obj interface{}) {
}

func swk8sPodDel(obj interface{}) {
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
	wt := 100 * time.Millisecond
	till := time.Now().Add(PodStartTmo)
	for {
		conn, err := net.Dial("tcp", addr + ":" + port)
		if err == nil {
			conn.Close()
			break
		}

		if time.Now().After(till) {
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
		}
		<-time.After(wt)
		wt += 50 * time.Millisecond
	}

	return nil
}

func swk8sPodUp(ctx context.Context, pod *k8sPod) error {
	bld := pod.Build()
	if bld != "" {
		ctxlog(ctx).Debugf("Update %s builder to %s", bld, pod.WdogAddr)
		RtSetBuilder(bld, pod.WdogAddr)
		return nil
	}

	err := BalancerPodUp(ctx, pod)
	if err != nil {
		ctxlog(ctx).Errorf("Can't prep pod %s/%s: %s", pod.DepName, pod.UID, err.Error())
		return err
	}

	go func() {
		ctx, done := mkContext("::podwait")
		defer done(ctx)

		err = waitPodPort(ctx, pod.WdogAddr, pod.WdogPort)
		if err != nil {
			ctxlog(ctx).Errorf("POD %s port wait err: %s",
					pod.UID, err.Error())
			return
		}

		err = BalancerPodRdy(ctx, pod)
		if err != nil {
			ctxlog(ctx).Errorf("Can't add pod %s/%s/%s: %s",
					pod.DepName, pod.UID,
					pod.WdogAddr, err.Error())
			return
		}

		notifyPodUp(ctx, pod)
	}()

	return nil
}

func swk8sPodDown(ctx context.Context, pod *k8sPod) {
	if pod.Build() != "" {
		return
	}

	ctxlog(ctx).Debugf("POD %s down deploy %s", pod.UID, pod.DepName)

	err := BalancerPodDel(ctx, pod)
	if err != nil {
		ctxlog(ctx).Errorf("Can't delete pod %s/%s/%s: %s",
				pod.DepName, pod.UID,
				pod.WdogAddr, err.Error())
	}
}

func swk8sPodUpd(obj_old, obj_new interface{}) {
	po := obj_old.(*v1.Pod)
	pn := obj_new.(*v1.Pod)

/*
 *	poc := ""
 *	pnc := ""
 *	for _, cond := range po.Status.Conditions {
 *		poc += string(cond.Type) + "=" + string(cond.Status) + ","
 *	}
 *	for _, cond := range pn.Status.Conditions {
 *		pnc += string(cond.Type) + "=" + string(cond.Status) + ","
 *	}
 *
 *	glog.Debugf("POD events\n%s:%s:%s:%s:%s ->\n%s:%s:%s:%s:%s\n",
 *		po.ObjectMeta.Labels["deployment"], po.ObjectMeta.UID, po.Status.PodIP, po.Status.Phase, poc,
 *		pn.ObjectMeta.Labels["deployment"], pn.ObjectMeta.UID, pn.Status.PodIP, pn.Status.Phase, pnc)
 */

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
		if evt.up {
			ctxlog(ctx).Debugf("POD %s (%s) up deploy %s",
					evt.pod.UID, evt.pod.WdogAddr, evt.pod.DepName)
			swk8sPodUp(ctx, evt.pod)
		} else {
			swk8sPodDown(ctx, evt.pod)
		}
		done(ctx)
	}
}

func init() {
	podEvents = make(chan *podEvent)
	go podEventLoop()
}

func swk8sMwSecretAdd(ctx context.Context, id string, envs map[string][]byte) error {
	secrets := swk8sClientSet.Secrets(conf.Wdog.Namespace)
	_, err := secrets.Create(&v1.Secret{
			ObjectMeta:	v1.ObjectMeta {
				Name:	"mw-" + id,
				Labels:	map[string]string{},
			},
			Data:		envs,
			Type:		v1.SecretTypeOpaque,
		})

	if err != nil {
		ctxlog(ctx).Errorf("mware secret add error: %s", err.Error())
		err = errors.New("K8S error")
	}

	return err
}

func swk8sMwSecretRemove(ctx context.Context, id string) error {
	var orphan bool = false
	var grace int64 = 0
	var err error

	secrets := swk8sClientSet.Secrets(conf.Wdog.Namespace)
	err = secrets.Delete("mw-" + id,
		&v1.DeleteOptions{
			GracePeriodSeconds: &grace,
			OrphanDependents: &orphan,
		})
	if err != nil {
		ctxlog(ctx).Errorf("Can't remove mw %s secret: %s", id, err.Error())
		if k8serr.IsNotFound(err) {
			err = nil
		}
	}

	return err
}

func swk8sDepScale(depname string, replicas int32, up bool) int32 {
	deps := swk8sClientSet.Extensions().Deployments(conf.Wdog.Namespace)
	dep, err := deps.Get(depname)
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

func swk8sDepScaleUp(depname string, replicas uint32) uint32 {
	return uint32(swk8sDepScale(depname, int32(replicas), true))
}

func swk8sDepScaleDown(depname string, replicas uint32) uint32 {
	return uint32(swk8sDepScale(depname, int32(replicas), false))
}

func swk8sGetBuildPods() (map[string]string, error) {
	rv := make(map[string]string)

	podiface := swk8sClientSet.Pods(conf.Wdog.Namespace)
	pods, err := podiface.List(v1.ListOptions{ LabelSelector: "swybuild" })
	if err != nil {
		glog.Errorf("Error listing PODs: %s", err.Error())
		return nil, errors.New("Error listing PODs")
	}

	for _, pod := range pods.Items {
		build := pod.ObjectMeta.Labels["swybuild"]
		rv[build] = pod.Status.PodIP
		glog.Debugf("Found building POD %s/%s\n", build, pod.Status.PodIP)
	}

	return rv, nil
}

func refreshDepsAndPods(ctx context.Context) error {
	var fns []*FunctionDesc

	err := dbFindAll(ctx, bson.M{}, &fns)
	if err != nil {
		return errors.New("Error listing FNs")
	}

	err = dbBalancerPodDelStuck(ctx)
	if err != nil {
		return fmt.Errorf("Can't drop stuck PODs: %s", err.Error)
	}

	depiface := swk8sClientSet.Extensions().Deployments(conf.Wdog.Namespace)
	podiface := swk8sClientSet.Pods(conf.Wdog.Namespace)

	for _, fn := range(fns) {
		if fn.State != swy.DBFuncStateRdy && fn.State != swy.DBFuncStateStr {
			continue
		}

		err := dbBalancerPodDelAll(ctx, fn.Cookie)
		if err != nil {
			glog.Errorf("Can't flush PODs info: %s", err.Error())
			return err
		}

		dep, err := depiface.Get(fn.DepName())
		if err != nil {
			if !k8serr.IsNotFound(err) {
				glog.Errorf("Can't get dep %s: %s", fn.DepName(), err.Error())
				return errors.New("Error getting dep")
			}

			if fn.State == swy.DBFuncStateStr {
				/* That's OK, the deployment just didn't have time to
				 * to get created. Just create one and ... go agead,
				 * no replicas to check, no PODs to revitalize.
				 */

				err = swk8sRun(ctx, &conf, fn)
				if err != nil {
					glog.Errorf("Can't start back %s dep: %s", fn.SwoId.Str(), err.Error())
					return err
				}

				continue
			}

			if fn.State == swy.DBFuncStateRdy {
				/* Function is running, but the deploy is not there
				 * Mark FN as stalled and let client handle it
				 */

				 fn.ToState(ctx, swy.DBFuncStateStl, -1)
				 continue
			}

		}

		glog.Debugf("Chk replicas for %s", fn.SwoId.Str())
		if *dep.Spec.Replicas > 1 {
			glog.Debugf("Found grown-up (%d) deployment %s", *dep.Spec.Replicas, dep.Name)
			err = scalerInit(ctx, fn, uint32(*dep.Spec.Replicas))
			if err != nil {
				glog.Errorf("Can't reinit scaler: %s", err.Error())
				return err
			}
		}

		pods, err := podiface.List(v1.ListOptions{ LabelSelector: "swyrun=" + fn.Cookie[:32] })
		if err != nil {
			glog.Errorf("Error listing PODs: %s", err.Error())
			return errors.New("Error listing PODs")
		}

		glog.Debugf("Chk PODs for %s", fn.SwoId.Str())
		for _, pod := range pods.Items {
			glog.Debugf("Found pod %s %s\n", pod.Name, pod.Status.PodIP)
			err = swk8sPodUp(ctx, genBalancerPod(&pod))
			if err != nil {
				glog.Errorf("Can't refresh POD: %s", err.Error())
				return err
			}
		}
	}

	return nil
}

func swk8sInit(ctx context.Context, conf *YAMLConf, config_path string) error {
	config_path = filepath.Dir(config_path) + "/kubeconfig"
	kubeconfig := flag.String("kubeconfig", config_path, "path to the kubeconfig file")
	flag.Parse()

	if conf.Wdog.Namespace == "" {
		glog.Debugf("Will work on %s k8s namespace", v1.NamespaceDefault)
		conf.Wdog.Namespace = v1.NamespaceDefault
	}

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		glog.Errorf("BuildConfigFromFlags: %s", err.Error())
		return err
	}

	swk8sClientSet, err = kubernetes.NewForConfig(config)
	if err != nil {
		glog.Errorf("NewForConfig: %s", err.Error())
		return err
	}

	watchlist := cache.NewListWatchFromClient(swk8sClientSet.Core().RESTClient(),
							"pods", conf.Wdog.Namespace,
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

	err = refreshDepsAndPods(ctx)
	if err != nil {
		glog.Errorf("Can't sart scaler: %s", err.Error())
		return err
	}

	return nil
}
