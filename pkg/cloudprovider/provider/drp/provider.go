package drp

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/kubermatic/machine-controller/pkg/cloudprovider/cloud"
	cloudprovidererrors "github.com/kubermatic/machine-controller/pkg/cloudprovider/errors"
	"github.com/kubermatic/machine-controller/pkg/cloudprovider/instance"
	"github.com/kubermatic/machine-controller/pkg/providerconfig"

	"k8s.io/apimachinery/pkg/types"

	gocache "github.com/patrickmn/go-cache"

	"github.com/digitalrebar/provision/api"
	"github.com/digitalrebar/provision/models"

	"sigs.k8s.io/cluster-api/pkg/apis/cluster/common"
	"sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
)

type provider struct {
	configVarResolver *providerconfig.ConfigVarResolver
}

// New returns a aws provider
func New(configVarResolver *providerconfig.ConfigVarResolver) cloud.Provider {
	return &provider{configVarResolver: configVarResolver}
}

const (
	nameTag       = "Name"
	machineUIDTag = "Machine-UID"

	maxRetries = 100
)

var (
	// cacheLock protects concurrent cache misses against a single key. This usually happens when multiple machines get created simultaneously
	// We lock so the first access updates/writes the data to the cache and afterwards everyone reads the cached data
	cacheLock = &sync.Mutex{}
	cache     = gocache.New(5*time.Minute, 5*time.Minute)
)

type RawConfig struct {
	Endpoint providerconfig.ConfigVarString `json:"endpoint"`
	Token    providerconfig.ConfigVarString `json:"token"`
	Pool     providerconfig.ConfigVarString `json:"pool"`

	CentosWorkflow providerconfig.ConfigVarString `json:"centosWorkflow"`
	CoreosWorkflow providerconfig.ConfigVarString `json:"coreosWorkflow"`
	UbuntuWorkflow providerconfig.ConfigVarString `json:"ubuntuWorkflow"`
}

type Config struct {
	Endpoint string
	Token    string
	Pool     string

	CentosWorkflow string
	CoreosWorkflow string
	UbuntuWorkflow string
}

func (p *provider) getConfig(s v1alpha1.ProviderConfig) (*Config, *providerconfig.Config, error) {
	if s.Value == nil {
		return nil, nil, fmt.Errorf("machine.spec.providerconfig.value is nil")
	}
	pconfig := providerconfig.Config{}
	err := json.Unmarshal(s.Value.Raw, &pconfig)
	if err != nil {
		return nil, nil, err
	}
	rawConfig := RawConfig{}
	if err := json.Unmarshal(pconfig.CloudProviderSpec.Raw, &rawConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal: %v", err)
	}
	c := Config{}
	c.Endpoint, err = p.configVarResolver.GetConfigVarStringValueOrEnv(rawConfig.Endpoint, "RS_ENDPOINT")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get the value of \"endpoint\" field, error = %v", err)
	}
	c.Token, err = p.configVarResolver.GetConfigVarStringValueOrEnv(rawConfig.Token, "RS_TOKEN")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get the value of \"token\" field, error = %v", err)
	}
	c.Pool, err = p.configVarResolver.GetConfigVarStringValueOrEnv(rawConfig.Pool, "RS_POOL")
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get the value of \"pool\" field, error = %v", err)
	}

	// GREG: Process Workflows at some point

	return &c, &pconfig, err
}

func (p *provider) AddDefaults(spec v1alpha1.MachineSpec) (v1alpha1.MachineSpec, error) {
	fmt.Printf("GREG: AddDefaults: %v\n", spec)
	return spec, nil
}

func (p *provider) Validate(spec v1alpha1.MachineSpec) error {
	fmt.Printf("GREG: Validate: %v\n", spec)

	config, pc, err := p.getConfig(spec.ProviderConfig)
	if err != nil {
		return fmt.Errorf("failed to parse config: %v", err)
	}

	fmt.Printf("GREG: Validate: config = %v\n", config)
	fmt.Printf("GREG: Validate: pc = %v\n", pc)

	// GREG: Should we check workflows here?

	return nil
}

func (p *provider) Create(machine *v1alpha1.Machine, data *cloud.MachineCreateDeleteData, userdata string) (instance.Instance, error) {
	fmt.Printf("GREG: Create: machine: %v\ndata: %v\nuserdata: %v\n", machine, data, userdata)

	config, _, err := p.getConfig(machine.Spec.ProviderConfig)
	if err != nil {
		return nil, cloudprovidererrors.TerminalError{
			Reason:  common.InvalidConfigurationMachineError,
			Message: fmt.Sprintf("Failed to parse MachineSpec, due to %v", err),
		}
	}
	fmt.Printf("GREG: Create: config = %v\n", config)

	m := &models.Machine{
		Name: machine.Spec.Name,
		Params: map[string]interface{}{
			"machine-controller/uid": string(machine.UID),
			"cloud-init/user-data":   string(userdata),
			"machine-plugin":         "packet-ipmi",
		},
	}
	session, err := api.TokenSession(config.Endpoint, config.Token)
	if err != nil {
		return nil, cloudprovidererrors.TerminalError{
			Reason:  common.InvalidConfigurationMachineError,
			Message: fmt.Sprintf("Failed to connect to the endpoint: %v", err),
		}
	}

	// GREG: This should be a pool call one day.
	if err := session.CreateModel(m); err != nil {
		return nil, cloudprovidererrors.TerminalError{
			Reason:  common.InvalidConfigurationMachineError,
			Message: fmt.Sprintf("Failed to create : %v", err),
		}
	}

	return &drpInstance{instance: m}, nil
}

func (p *provider) Cleanup(machine *v1alpha1.Machine, kk *cloud.MachineCreateDeleteData) (bool, error) {
	fmt.Printf("GREG: Cleanup: machine: %v\nkk: %v\n", machine, kk)

	instance, err := p.Get(machine)
	if err != nil {
		if err == cloudprovidererrors.ErrInstanceNotFound {
			return true, nil
		}
		return false, err
	}

	config, _, err := p.getConfig(machine.Spec.ProviderConfig)
	if err != nil {
		return false, cloudprovidererrors.TerminalError{
			Reason:  common.InvalidConfigurationMachineError,
			Message: fmt.Sprintf("Failed to parse MachineSpec, due to %v", err),
		}
	}
	fmt.Printf("GREG: Cleanup: config = %v\n", config)
	fmt.Printf("GREG: Cleanup: instance = %v\n", instance)

	session, err := api.TokenSession(config.Endpoint, config.Token)
	if err != nil {
		return false, cloudprovidererrors.TerminalError{
			Reason:  common.InvalidConfigurationMachineError,
			Message: fmt.Sprintf("Failed to connect to the endpoint: %v", err),
		}
	}

	// GREG: Release machine to pool
	name := fmt.Sprintf("Name:%s", instance.Name())
	if _, err := session.DeleteModel("machines", name); err != nil {
		return false, cloudprovidererrors.TerminalError{
			Reason:  common.InvalidConfigurationMachineError,
			Message: fmt.Sprintf("Failed to delete %s %v", name, err),
		}
	}

	return true, nil
}

func (p *provider) Get(machine *v1alpha1.Machine) (instance.Instance, error) {
	fmt.Printf("GREG: Get: machine: %v\n", machine)

	config, _, err := p.getConfig(machine.Spec.ProviderConfig)
	if err != nil {
		return nil, cloudprovidererrors.TerminalError{
			Reason:  common.InvalidConfigurationMachineError,
			Message: fmt.Sprintf("Failed to parse MachineSpec, due to %v", err),
		}
	}
	fmt.Printf("GREG: Get: config = %v\n", config)

	session, err := api.TokenSession(config.Endpoint, config.Token)
	if err != nil {
		return nil, cloudprovidererrors.TerminalError{
			Reason:  common.InvalidConfigurationMachineError,
			Message: fmt.Sprintf("Failed to connect to the endpoint: %v", err),
		}
	}

	// GREG: Find machine by their UID and then return it
	l, err := session.ListModel("machines", "machine-controller/uid", fmt.Sprintf("Eq(%s)", string(machine.UID)))
	if err != nil {
		return nil, cloudprovidererrors.TerminalError{
			Reason:  common.InvalidConfigurationMachineError,
			Message: fmt.Sprintf("Failed to list machines: %v", err),
		}
	}

	for _, om := range l {
		m, _ := om.(*models.Machine)
		return &drpInstance{instance: m}, nil
	}

	return nil, cloudprovidererrors.ErrInstanceNotFound
}

func (p *provider) GetCloudConfig(spec v1alpha1.MachineSpec) (config string, name string, err error) {
	// GREG: Specific here;
	fmt.Printf("GREG: GetCloudConfig: %v\n", spec)
	return "", "drp", nil

}

func (p *provider) MachineMetricsLabels(machine *v1alpha1.Machine) (map[string]string, error) {
	fmt.Printf("GREG: MachineMetricsLabels: %v\n", machine)
	labels := make(map[string]string)
	return labels, nil
}

func (p *provider) MigrateUID(machine *v1alpha1.Machine, new types.UID) error {
	fmt.Printf("GREG: MigrateUID: %v\nUUID:%v\n", machine, new)

	instance, err := p.Get(machine)
	if err != nil {
		if err == cloudprovidererrors.ErrInstanceNotFound {
			return nil
		}
		return fmt.Errorf("failed to get instance: %v", err)
	}

	config, _, err := p.getConfig(machine.Spec.ProviderConfig)
	if err != nil {
		return cloudprovidererrors.TerminalError{
			Reason:  common.InvalidConfigurationMachineError,
			Message: fmt.Sprintf("Failed to parse MachineSpec, due to %v", err),
		}
	}
	fmt.Printf("GREG: MigrateUID: config = %v\n", config)
	fmt.Printf("GREG: MigrateUID: instance = %v\n", instance)

	// GREG: Update the machine's stored MC UID

	return nil
}

type drpInstance struct {
	instance *models.Machine
}

func (d *drpInstance) Name() string {
	return d.instance.Name
}

func (d *drpInstance) ID() string {
	uid, _ := d.instance.Params["machine-controller/uid"].(string)
	return uid
}

func (d *drpInstance) Addresses() []string {
	return []string{
		d.instance.Address.String(),
	}
}

func (d *drpInstance) Status() instance.Status {
	fmt.Printf("GREG: calling status on %s: %s\n", d.instance.Name, d.instance.Stage)
	return instance.StatusRunning
	/* GREG: This needs thought
	switch *d.instance.State.Name {
	case ec2.InstanceStateNameRunning:
		return instance.StatusRunning
	case ec2.InstanceStateNamePending:
		return instance.StatusCreating
	case ec2.InstanceStateNameTerminated:
		return instance.StatusDeleted
	case ec2.InstanceStateNameShuttingDown:
		return instance.StatusDeleting
	default:
		return instance.StatusUnknown
	}
	*/
}
