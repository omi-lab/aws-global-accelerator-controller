package controller

import (
	"context"
	"os"

	"github.com/h3poteto/aws-global-accelerator-controller/pkg/cloudprovider/aws"
	"github.com/h3poteto/aws-global-accelerator-controller/pkg/controller/endpointgroupbinding"
	"github.com/h3poteto/aws-global-accelerator-controller/pkg/controller/globalaccelerator"
	"github.com/h3poteto/aws-global-accelerator-controller/pkg/controller/route53"
	"github.com/h3poteto/aws-global-accelerator-controller/pkg/leaderelection"
	"github.com/h3poteto/aws-global-accelerator-controller/pkg/manager"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

type options struct {
	workers           int
	clusterName       string
	annotationFilters map[string]string
	route53ZoneType   string
	route53TXTOwnerID string
}

func ControllerCmd() *cobra.Command {
	o := &options{}
	cmd := &cobra.Command{
		Use:   "controller",
		Short: "Start controller",
		Run:   o.run,
	}
	flags := cmd.Flags()
	flags.IntVarP(&o.workers, "workers", "w", 1, "Concurrent workers number for controller.")
	flags.StringVarP(&o.clusterName, "cluster-name", "c", "default", "Owner cluster name which is used in resource tags.")
	flags.StringToStringVar(&o.annotationFilters, "annotation-filter", map[string]string{}, "A map of filters to apply when listing ingresses/services")
	flags.StringVar(&o.route53ZoneType, "route53-zone-type", "", "The type of zone(s) to filter in ('public' or 'private').")
	flags.StringVar(&o.route53TXTOwnerID, "route53-txt-owner-id", "aws-global-accelerator-controller", "ID of the owner for the txt records. Use it if you have multiple deployments of the controller managing the same Route53 zone.")

	cmd.PersistentFlags().String("kubeconfig", "", "Path to a kubeconfig. Only required if out-of-cluster.")
	cmd.PersistentFlags().String("master", "", "The address of the Kubernetes API server. Overrides any value in kubeconfig. Only required if out-of-cluster.")
	_ = viper.BindPFlag("kubeconfig", cmd.PersistentFlags().Lookup("kubeconfig"))
	_ = viper.BindPFlag("master", cmd.PersistentFlags().Lookup("master"))

	return cmd
}

func (o *options) run(cmd *cobra.Command, args []string) {
	kubeconfig, masterURL := controllerConfig()
	if kubeconfig != "" {
		klog.Infof("Using kubeconfig: %s", kubeconfig)
	} else {
		klog.Info("Using in-cluster config")
	}
	cfg, err := clientcmd.BuildConfigFromFlags(masterURL, kubeconfig)
	if err != nil {
		klog.Fatalf("Error building rest config: %s", err.Error())
	}

	ns := os.Getenv("POD_NAMESPACE")
	if ns == "" {
		ns = "default"
	}

	var route53ZoneType aws.ZoneType
	switch o.route53ZoneType {
	case string(aws.ZoneTypePublic):
		route53ZoneType = aws.ZoneTypePublic
	case string(aws.ZoneTypePrivate):
		route53ZoneType = aws.ZoneTypePrivate
	}

	config := manager.ControllerConfig{
		GlobalAccelerator: &globalaccelerator.GlobalAcceleratorConfig{
			Workers:     o.workers,
			ClusterName: o.clusterName,
		},
		Route53: &route53.Route53Config{
			Workers:           o.workers,
			ClusterName:       o.clusterName,
			AnnotationFilters: o.annotationFilters,
			TXTOwnerID:        o.route53TXTOwnerID,
			ZoneType:          route53ZoneType,
		},
		EndpointGroupBinding: &endpointgroupbinding.EndpointGroupBindingConfig{
			Workers: o.workers,
		},
	}

	le := leaderelection.NewLeaderElection("aws-global-accelerator-controller", ns)
	ctx := context.Background()
	err = le.Run(ctx, cfg, func(ctx context.Context, clientConfig *rest.Config, stopCh <-chan struct{}) {
		m := manager.NewManager()
		if err := m.Run(ctx, clientConfig, &config, stopCh); err != nil {
			klog.Fatalf("Error running controller: %v", err)
		}
	})
	klog.Fatalf("Error starting controller: %s", err.Error())
}

func controllerConfig() (string, string) {
	kubeconfig := viper.GetString("kubeconfig")
	if kubeconfig == "" {
		kubeconfig = os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = os.ExpandEnv("$HOME/.kube/config")
			if _, err := os.Stat(kubeconfig); err != nil {
				klog.Error(err)
				kubeconfig = ""
			}
		}
	}
	master := viper.GetString("master")
	return kubeconfig, master
}
