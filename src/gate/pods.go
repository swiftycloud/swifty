package main

import (
	"context"
)

func podsAdd(ctx context.Context, pod *k8sPod) error {
	return dbBalancerPodAdd(ctx, pod)
}

func podsDel(ctx context.Context, pod *k8sPod) error {
	return dbBalancerPodDel(ctx, pod)
}

func podsRdy(ctx context.Context, fnid string, pod *k8sPod) error {
	return dbBalancerPodUpd(ctx, fnid, pod)
}

func podsDelAll(ctx context.Context, fnid string) error {
	return dbBalancerPodDelAll(ctx, fnid)
}

func podsDelStuck(ctx context.Context) error {
	return dbBalancerPodDelStuck(ctx)
}

func podsFindExact(ctx context.Context, fnid, version string) (*podConn, error) {
	return dbBalancerGetConnExact(ctx, fnid, version)
}

func podsFindAll(ctx context.Context, fnid string) ([]*podConn, error) {
	return dbBalancerGetConnsByCookie(ctx, fnid)
}

func podsListVersions(ctx context.Context, fnid string) ([]string, error) {
	return dbBalancerListVersions(ctx, fnid)
}
