package azure

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2018-01-01/network"
	log "github.com/zoumo/logdog"

	"github.com/caicloud/loadbalancer-provider/providers/azure/client"
)

const (
	APPGATEWAY      = "loadbalance.caicloud.io/azureAppGateway"
	APPGATEWAY_NAME = "loadbalance.caicloud.io/azureAppGatewayName"
	RESOURCE_GROUP  = "loadbalance.caicloud.io/azureResourceGroup"
)

func getAzureAppGateway(c *client.Client, groupName, appGatewayName string) (*network.ApplicationGateway, error) {
	if len(appGatewayName) == 0 {
		return nil, nil
	}
	ag, err := c.AppGateway.Get(context.TODO(), groupName, appGatewayName)
	if client.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		log.Errorf("get appGateway %s failed: %v", appGatewayName, err)
		return nil, err
	}
	return &ag, nil
}

func addAppGatewayBackendPool(c *client.Client, nodeip []network.ApplicationGatewayBackendAddress, groupName, agName, lb string) error {
	ag, err := getAzureAppGateway(c, groupName, agName)
	if err != nil || ag == nil {
		log.Errorf("get application gateway error %v", err)
		return err
	}

	poolName := lb + "-backendpool"
	*ag.ApplicationGatewayPropertiesFormat.BackendAddressPools = append((*ag.ApplicationGatewayPropertiesFormat.BackendAddressPools)[:], network.ApplicationGatewayBackendAddressPool{
		Name: &poolName,
		ApplicationGatewayBackendAddressPoolPropertiesFormat: &network.ApplicationGatewayBackendAddressPoolPropertiesFormat{
			BackendAddresses: &nodeip,
		},
	})

	_, err = c.AppGateway.CreateOrUpdate(context.TODO(), groupName, agName, *ag)
	if err != nil {
		log.Errorf("update application gateway error %v", err)
		return err
	}

	return nil
}

func deleteAppGatewayBackendPool(c *client.Client, groupName, agName, lb string) error {
	ag, err := getAzureAppGateway(c, groupName, agName)
	if err != nil || ag == nil {
		log.Errorf("get application gateway error %v", err)
		return err
	}

	poolName := lb + "-backendpool"
	var bp []network.ApplicationGatewayBackendAddressPool
	for _, pool := range *ag.ApplicationGatewayPropertiesFormat.BackendAddressPools {
		if *pool.Name != poolName {
			bp = append(bp, pool)
		}
	}
	*ag.ApplicationGatewayPropertiesFormat.BackendAddressPools = bp
	_, err = c.AppGateway.CreateOrUpdate(context.TODO(), groupName, agName, *ag)
	if err != nil {
		log.Errorf("update application gateway error %v", err)
		return err
	}

	return nil
}

func updateAppGatewayBackendPoolIP(c *client.Client, nodeip []network.ApplicationGatewayBackendAddress, groupName, agName, lb string) error {
	ag, err := getAzureAppGateway(c, groupName, agName)
	if err != nil || ag == nil {
		log.Errorf("get application gateway error %v", err)
		return err
	}

	poolName := lb + "-backendpool"
	for index, pool := range *ag.ApplicationGatewayPropertiesFormat.BackendAddressPools {
		if *pool.Name == poolName {
			(*ag.ApplicationGatewayPropertiesFormat.BackendAddressPools)[index] = network.ApplicationGatewayBackendAddressPool{
				Name: &poolName,
				ApplicationGatewayBackendAddressPoolPropertiesFormat: &network.ApplicationGatewayBackendAddressPoolPropertiesFormat{
					BackendAddresses: &nodeip,
				},
			}
		}
	}

	_, err = c.AppGateway.CreateOrUpdate(context.TODO(), groupName, agName, *ag)
	if err != nil {
		log.Errorf("update application gateway error %v", err)
		return err
	}

	return nil

}
