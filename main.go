package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/blang/semver"
	"github.com/heroku/docker-registry-client/registry"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	dockerHubURL = "https://registry-1.docker.io/"
)

var (
	kubeconfig = flag.String("kubeconfig", "", "Absolute path to the kubeconfig file, otherwise assume running in-cluster.")
)

type imageName struct {
	registry   string
	namespace  string
	repository string
	tag        string
	digest     string
}

func parseImage(name string) *imageName {
	names := strings.Split(name, "/")

	ret := imageName{
		namespace: "library",
	}
	switch len(names) {
	case 1:
		ret.repository = names[0]
	case 2:
		if strings.ContainsAny(names[0], ":.") {
			ret.registry = names[0]
		} else {
			ret.namespace = names[0]
		}
		ret.repository = names[1]
	case 3:
		ret.registry = names[0]
		ret.namespace = names[1]
		ret.repository = names[2]
	default:
		return nil
	}

	if i := strings.Index(ret.repository, "@"); i != -1 {
		ret.digest = ret.repository[i+1:]
		ret.repository = ret.repository[:i]
	} else if i := strings.Index(ret.repository, ":"); i != -1 {
		ret.tag = ret.repository[i+1:]
		ret.repository = ret.repository[:i]
	} else {
		ret.tag = "latest"
	}

	return &ret
}

func (i imageName) String() string {
	s := ""
	if i.registry != "" {
		s += i.registry + "/"
	}
	if i.namespace != "" {
		s += i.namespace + "/"
	}
	s += i.repository
	if i.digest != "" {
		s += "@" + i.digest
	} else if i.tag != "" {
		s += ":" + i.tag
	}

	return s
}

func tagSemVer(tag string) (semver.Version, error) {
	tag = strings.TrimPrefix(tag, "v")
	for strings.Count(tag, ".") < 2 {
		tag += ".0"
	}
	return semver.Parse(tag)
}

type registryCache map[string]*registry.Registry

func (r *registryCache) Get(reg string) (*registry.Registry, error) {
	if hub := (*r)[reg]; hub != nil {
		log.Printf("hit for %q: %v", reg, hub)
		return hub, nil
	}

	url := dockerHubURL
	if reg != "" {
		url = "https://" + reg
	}

	hub, err := registry.New(url, "", "")
	if err != nil {
		return nil, err
	}

	(*r)[reg] = hub
	log.Printf("miss for %q: %v", reg, hub)
	return hub, nil
}

func main2() error {
	var conf *rest.Config
	var err error
	if *kubeconfig == "" {
		conf, err = rest.InClusterConfig()
	} else {
		conf, err = clientcmd.BuildConfigFromFlags("", *kubeconfig)
	}
	if err != nil {
		return err
	}

	clientset, err := kubernetes.NewForConfig(conf)
	if err != nil {
		return err
	}

	pods, err := clientset.CoreV1().Pods(metav1.NamespaceAll).List(metav1.ListOptions{})
	if err != nil {
		return err
	}

	images := map[string][]*v1.Pod{}
	for i := range pods.Items {
		pod := &pods.Items[i]
		podImages := map[string]struct{}{}
		for _, c := range pod.Spec.InitContainers {
			podImages[c.Image] = struct{}{}
		}
		for _, c := range pod.Spec.Containers {
			podImages[c.Image] = struct{}{}
		}

		for img := range podImages {
			images[img] = append(images[img], pod)
		}
	}

	regCache := registryCache{}

	for imgName, pods := range images {
		img := parseImage(imgName)
		if img == nil {
			log.Printf("Unable to parse image: %q", imgName)
			continue
		}
		log.Printf("Image: %s", img)

		tag := img.tag
		if tag == "" {
			// digest-based image, skip
			continue
		}
		ver, err := tagSemVer(tag)
		if err != nil {
			log.Printf("Unable to parse %q as semver: %v", tag, err)
			// TODO: compare tag digests vs repo (ie: "latest")
			continue
		}

		hub, err := regCache.Get(img.registry)
		if err != nil {
			log.Printf("Unable to connect to registry %s: %v", img, err)
			continue
		}
		tags, err := hub.Tags(fmt.Sprintf("%s/%s", img.namespace, img.repository))
		if err != nil {
			log.Printf("Unable to fetch tags for %s: %v", img, err)
			continue
		}

		for _, t := range tags {
			tagv, err := tagSemVer(t)
			if err != nil {
				log.Printf("Skipping non-semver %q: %v", t, err)
				continue
			}

			if tagv.GT(ver) {
				log.Printf("** Newer tag found: %s", tagv)
			}
		}

		for _, pod := range pods {
			log.Printf("Used by: %s/%s", pod.GetNamespace(), pod.GetName())
		}
	}

	return nil
}

func main() {
	flag.Parse()

	if err := main2(); err != nil {
		panic(err)
	}
}
