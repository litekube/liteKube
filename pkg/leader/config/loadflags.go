package config

import (
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/litekube/LiteKube/pkg/certificate"
	"github.com/litekube/LiteKube/pkg/leader/authentication"
	options "github.com/litekube/LiteKube/pkg/options/leader"
	"github.com/litekube/LiteKube/pkg/options/leader/apiserver"
	globaloptions "github.com/litekube/LiteKube/pkg/options/leader/global"
	kineoptions "github.com/litekube/LiteKube/pkg/options/leader/kine"
	"github.com/litekube/LiteKube/pkg/options/leader/netmanager"
	"gopkg.in/yaml.v2"
	"k8s.io/klog/v2"
)

func (runtime *LeaderRuntime) SetFlags(opt *options.LeaderOptions) {
	runtime.FlagsOption = opt
}

// load all flags
func (runtime *LeaderRuntime) LoadFlags() error {
	if runtime.FlagsOption == nil {
		return fmt.Errorf("no flags input")
	}

	// init global flags
	if err := runtime.LoadGloabl(); err != nil {
		return err
	}

	// init kine flags
	if err := runtime.LoadKine(); err != nil {
		return err
	}

	// init network manager flags
	if err := runtime.LoadNetManager(); err != nil {
		return err
	}

	if config, err := yaml.Marshal(runtime.RuntimeOption); err != nil {
		return err
	} else {
		startupDir := filepath.Join(runtime.RuntimeOption.GlobalOptions.WorkDir, "startup/")
		if err := os.MkdirAll(startupDir, os.ModePerm); err != nil {
			return err
		}
		if err := ioutil.WriteFile(filepath.Join(startupDir, "leader.yaml"), config, os.ModePerm); err != nil {
			return err
		}
	}
	return nil
}

// load or generate args for litekube-global
func (runtime *LeaderRuntime) LoadGloabl() error {
	defer func() {
		runtime.RuntimeAuthentication = NewRuntimeAuthentication(filepath.Join(runtime.RuntimeOption.GlobalOptions.WorkDir, "tls/"))
	}()

	raw := runtime.FlagsOption.GlobalOptions
	new := runtime.RuntimeOption.GlobalOptions

	// set default work-dir="~/litekube/"
	new.WorkDir = raw.WorkDir
	if new.WorkDir == "" {
		new.WorkDir = globaloptions.DefaultGO.WorkDir
	}

	// log
	new.LogDir = raw.LogDir
	if new.LogDir == "" {
		new.LogDir = filepath.Join(new.WorkDir, "logs/")
	}

	new.LogToDir = raw.LogToDir
	new.LogToStd = raw.LogToStd

	// kine
	new.RunKine = raw.RunKine
	// invalid etcd server will enable kine
	if runtime.RuntimeOption.ApiserverOptions.ProfessionalOptions.ECTDOptions.EtcdServers == "" {
		new.RunKine = true
	}

	// network-manager
	new.RunNetManager = raw.RunNetManager

	new.EnableWorker = raw.EnableWorker

	if !new.EnableWorker {
		new.WorkerConfig = ""
	}
	return nil
}

// load or generate args for kine server
// client certificate will be generate to path, too
func (runtime *LeaderRuntime) LoadKine() error {
	if !runtime.RuntimeOption.GlobalOptions.RunKine {
		runtime.RuntimeOption.KineOptions = nil
		return nil
	}

	raw := runtime.FlagsOption.KineOptions
	new := runtime.RuntimeOption.KineOptions

	// check bind-address
	if ip := net.ParseIP(raw.BindAddress); ip == nil {
		new.BindAddress = kineoptions.DefaultKO.BindAddress
	} else {
		new.BindAddress = raw.BindAddress
	}

	// check https port
	if raw.SecurePort < 1 || raw.SecurePort > 65535 {
		new.SecurePort = kineoptions.DefaultKO.SecurePort
	} else {
		new.SecurePort = raw.SecurePort
	}

	// check TLS certificate
	if certificate.NotExists(raw.CACert, raw.ServerCertFile, raw.ServerkeyFile) {
		klog.Info("built-in certificates for kine will be used")

		// invalid cert， generate kine certs
		runtime.OwnKineCert = true
		runtime.RuntimeAuthentication.Kine = authentication.NewKineAuthentication(runtime.RuntimeAuthentication.CertDir, new.BindAddress)
		if err := runtime.RuntimeAuthentication.Kine.GenerateOrSkip(); err != nil {
			return err
		}

		new.CACert = runtime.RuntimeAuthentication.Kine.CACert
		new.ServerCertFile = runtime.RuntimeAuthentication.Kine.ServerCert
		new.ServerkeyFile = runtime.RuntimeAuthentication.Kine.Serverkey
	} else {
		if !certificate.ValidateTLSPair(raw.ServerCertFile, raw.ServerkeyFile) || !certificate.ValidateCA(raw.ServerCertFile, raw.CACert) {
			klog.Errorf("You specified an unavailable certificate for kine")
			return fmt.Errorf("you specified an unavailable certificate for kine")
		}

		new.CACert = raw.CACert
		new.ServerCertFile = raw.ServerCertFile
		new.ServerkeyFile = raw.ServerkeyFile
		klog.Infof("kine certificate specified ok, skip generate")
	}

	return nil
}

// load network-manager client config
// if run-network-manager==true, runtime.RuntimeAuthentication.NetWorkManager will init
func (runtime *LeaderRuntime) LoadNetManager() error {
	raw := runtime.FlagsOption.NetmamagerOptions
	new := runtime.RuntimeOption.NetmamagerOptions

	// check bind-address
	if ip := net.ParseIP(raw.RegisterOptions.Address); ip == nil {
		new.RegisterOptions.Address = netmanager.DefaultRONO.Address
	} else {
		new.RegisterOptions.Address = raw.RegisterOptions.Address
	}

	if ip := net.ParseIP(raw.JoinOptions.Address); ip == nil {
		new.JoinOptions.Address = netmanager.DefaultJONO.Address
	} else {
		new.JoinOptions.Address = raw.JoinOptions.Address
	}

	// check https port
	if raw.RegisterOptions.SecurePort < 1 || raw.RegisterOptions.SecurePort > 65535 {
		new.RegisterOptions.SecurePort = netmanager.DefaultRONO.SecurePort
	} else {
		new.RegisterOptions.SecurePort = raw.RegisterOptions.SecurePort
	}

	// check https port
	if raw.JoinOptions.SecurePort < 1 || raw.JoinOptions.SecurePort > 65535 {
		new.JoinOptions.SecurePort = netmanager.DefaultJONO.SecurePort
	} else {
		new.JoinOptions.SecurePort = raw.JoinOptions.SecurePort
	}

	// check Token
	if runtime.RuntimeOption.GlobalOptions.RunNetManager {
		// generate certificate for network manager
		klog.Infof("certificates for built-in network manager server will be used")
		new.Token = "local"
		runtime.RuntimeAuthentication.NetWorkManager = authentication.NewNetworkAuthentication(runtime.RuntimeAuthentication.CertDir, new.RegisterOptions.Address, new.JoinOptions.Address)
		if err := runtime.RuntimeAuthentication.NetWorkManager.GenerateOrSkip(); err != nil {
			return err
		}
	} else {
		if raw.Token == "local" {
			return fmt.Errorf("bad token(local) to connect with network-manager, only enable when network manager run in leader node")
		} else if len(raw.Token) != 16 {
			return fmt.Errorf("error network token format")
		}

		new.Token = raw.Token
	}

	// try to load certificate provide by user
	if certificate.NotExists(raw.RegisterOptions.CACert, raw.RegisterOptions.ClientCertFile, raw.RegisterOptions.ClientkeyFile) && certificate.NotExists(raw.JoinOptions.CACert, raw.JoinOptions.ClientCertFile, raw.JoinOptions.ClientkeyFile) && raw.NodeToken == "" {
		// check client certificate
		// into TLS bootstrap
		klog.Infof("start load network manager client certificate and node-token by --token")
		runtime.RuntimeAuthentication.NetWorkManagerClient = authentication.NewNetworkManagerClient(runtime.RuntimeAuthentication.CertDir, new.Token)
		if err := runtime.RuntimeAuthentication.NetWorkManagerClient.GenerateOrSkip(new.RegisterOptions.Address, int(new.RegisterOptions.SecurePort)); err != nil {
			return err
		}

		if !runtime.RuntimeAuthentication.NetWorkManagerClient.Check() {
			return fmt.Errorf("fail to load network-manager TLS args")
		}

		// node token
		if nodeToken, err := runtime.RuntimeAuthentication.NetWorkManagerClient.Nodetoken(); err != nil {
			return err
		} else {
			new.NodeToken = nodeToken
		}

		// cert
		// join
		new.JoinOptions.CACert = runtime.RuntimeAuthentication.NetWorkManagerClient.JoinCACert
		new.JoinOptions.ClientCertFile = runtime.RuntimeAuthentication.NetWorkManagerClient.JoinClientCert
		new.JoinOptions.ClientkeyFile = runtime.RuntimeAuthentication.NetWorkManagerClient.JoinClientkey

		// register
		new.RegisterOptions.CACert = runtime.RuntimeAuthentication.NetWorkManagerClient.RegisterCACert
		new.RegisterOptions.ClientCertFile = runtime.RuntimeAuthentication.NetWorkManagerClient.RegisterClientCert
		new.RegisterOptions.ClientkeyFile = runtime.RuntimeAuthentication.NetWorkManagerClient.RegisterClientkey

		klog.Infof("success to load network manager client certificates node-token by --token")
		return nil

	} else {
		if certificate.ValidateTLSPair(raw.RegisterOptions.ClientCertFile, raw.RegisterOptions.ClientkeyFile) && certificate.ValidateCA(raw.RegisterOptions.ClientCertFile, raw.RegisterOptions.CACert) && certificate.ValidateTLSPair(raw.JoinOptions.ClientCertFile, raw.JoinOptions.ClientkeyFile) && certificate.ValidateCA(raw.JoinOptions.ClientCertFile, raw.JoinOptions.CACert) && len(raw.NodeToken) > 0 {
			// cert
			// join
			new.JoinOptions.CACert = raw.JoinOptions.CACert
			new.JoinOptions.ClientCertFile = raw.JoinOptions.ClientCertFile
			new.JoinOptions.ClientkeyFile = raw.JoinOptions.ClientkeyFile

			// register
			new.RegisterOptions.CACert = raw.RegisterOptions.CACert
			new.RegisterOptions.ClientCertFile = raw.RegisterOptions.ClientCertFile
			new.RegisterOptions.ClientkeyFile = raw.RegisterOptions.ClientkeyFile
			new.NodeToken = raw.NodeToken
			runtime.RuntimeAuthentication.NetWorkManagerClient = nil
			klog.Infof("network manager client certificates specified ok, ignore --token")
		} else {
			return fmt.Errorf("you have provide bad network manager client certificates or node-token for network manager")
		}
	}

	return nil
}

func (runtime *LeaderRuntime) LoadApiserver() error {
	raw := runtime.FlagsOption.ApiserverOptions
	new := runtime.RuntimeOption.ApiserverOptions

	new.ReservedOptions = raw.ReservedOptions
	new.IgnoreOptions = raw.IgnoreOptions

	// load *LitekubeOptions
	new.Options.AllowPrivileged = raw.Options.AllowPrivileged
	new.Options.AuthorizationMode = raw.Options.AuthorizationMode
	new.Options.AnonymousAuth = raw.Options.AnonymousAuth
	new.Options.EnableSwaggerUI = raw.Options.EnableSwaggerUI
	new.Options.EnableAdmissionPlugins = raw.Options.EnableAdmissionPlugins
	new.Options.EncryptionProviderConfig = raw.Options.EncryptionProviderConfig
	new.Options.Profiling = raw.Options.Profiling
	// check --service-cluster-ip-range
	if _, _, err := net.ParseCIDR(raw.Options.ServiceClusterIpRange); err != nil {
		new.Options.ServiceClusterIpRange = apiserver.DefaultALO.ServiceClusterIpRange
		new.IgnoreOptions["service-cluster-ip-range"] = raw.Options.ServiceClusterIpRange
	} else {
		new.Options.ServiceClusterIpRange = raw.Options.ServiceClusterIpRange
	}
	// check --service-node-port-range
	new.Options.ServiceNodePortRange = ""
	if ports := strings.Split(raw.Options.ServiceNodePortRange, "-"); len(ports) == 2 {
		port_min := 30000
		port_max := 65535
		if p, err := strconv.Atoi(strings.TrimSpace(ports[0])); err == nil && p > 0 {
			port_min = p
		}

		if p, err := strconv.Atoi(strings.TrimSpace(ports[1])); err == nil && p < 65536 {
			port_max = p
		}

		if port_max > port_min && (port_max-port_min) > 100 {
			new.Options.ServiceNodePortRange = fmt.Sprintf("%d-%d", port_min, port_max)
		}
	}

	if new.Options.ServiceNodePortRange == "" {
		// fail to parse port before
		new.Options.ServiceNodePortRange = apiserver.DefaultALO.ServiceNodePortRange
		new.IgnoreOptions["service-node-port-range"] = raw.Options.ServiceNodePortRange
	}

	if raw.Options.SecurePort < 1 || raw.Options.SecurePort > 65535 {
		new.Options.SecurePort = apiserver.DefaultALO.SecurePort
	} else {
		new.Options.SecurePort = raw.Options.SecurePort
	}

	return nil
}

func (runtime *LeaderRuntime) LoadControllermanager() error {
	return nil
}

func (runtime *LeaderRuntime) LoadScheduler() error {
	return nil
}

func (runtime *LeaderRuntime) LoadWorker() error {
	if !runtime.FlagsOption.GlobalOptions.EnableWorker {
		return nil
	}

	return nil
}