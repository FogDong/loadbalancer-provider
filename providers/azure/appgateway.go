package azure

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2018-01-01/network"
	"github.com/caicloud/loadbalancer-provider/providers/azure/client"
	log "github.com/zoumo/logdog"
	v1beta1 "k8s.io/api/extensions/v1beta1"
)

const (
	APPGATEWAY         = "loadbalance.caicloud.io/azureAppGateway"
	APPGATEWAY_NAME    = "loadbalance.caicloud.io/azureAppGatewayName"
	BACKENDPOOL_STATUS = "loadbalance.caicloud.io/azureBackendPoolStatus"
	RULE_STATUS        = "loadbalance.caicloud.io/azureRuleStatus"
	RULE_MSG           = "loadbalance.caicloud.io/azureRuleErrorMsg"
	ERROR_MSG          = "loadbalance.caicloud.io/azureErrorMsg"
	RESOURCE_GROUP     = "loadbalance.caicloud.io/azureResourceGroup"
)

func getAzureAppGateway(c *client.Client, groupName, appGatewayName string) (*network.ApplicationGateway, error) {
	if len(appGatewayName) == 0 {
		return nil, nil
	}
	ag, err := c.AppGateway.Get(context.TODO(), groupName, appGatewayName)
	if err != nil {
		return nil, err
	}

	return &ag, nil
}

func addAppGatewayBackendPool(c *client.Client, nodeip []network.ApplicationGatewayBackendAddress, groupName, agName, lb string, ingresses []*v1beta1.Ingress) error {
	ag, err := getAzureAppGateway(c, groupName, agName)
	if err != nil || ag == nil {
		log.Errorf("get application %s gateway error %v", agName, err)
		return err
	}

	poolName := lb + "-backendpool"
	*ag.ApplicationGatewayPropertiesFormat.BackendAddressPools = append((*ag.ApplicationGatewayPropertiesFormat.BackendAddressPools)[:], network.ApplicationGatewayBackendAddressPool{
		Name: &poolName,
		ApplicationGatewayBackendAddressPoolPropertiesFormat: &network.ApplicationGatewayBackendAddressPoolPropertiesFormat{
			BackendAddresses: &nodeip,
		},
	})

	if ingresses != nil {
		corres := make(map[string]int)
		for _, listener := range *ag.ApplicationGatewayPropertiesFormat.HTTPListeners {
			corres[*listener.Name] = -1
		}
		igInfo := make(map[string]string)
		for _, ig := range ingresses {
			if corres[ig.Name+"-cps-listener"] != -1 {
				igInfo[ig.Name] = ig.Spec.Rules[0].Host
			}
		}
		ag = addAllAzureRule(ag, lb, igInfo)
	}

	_, err = c.AppGateway.CreateOrUpdate(context.TODO(), groupName, agName, *ag)
	if err != nil {
		log.Errorf("add app gateway update application gateway error %v", err)
		return err
	}

	return nil
}

func deleteAppGatewayBackendPool(c *client.Client, groupName, agName, lb, rule string) error {
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

	if rule != "" {
		log.Info("deleting all azure rule")
		ruleStatus := strings.Replace(string(rule), "'", "\"", -1)
		rStatus := make(map[string]string)
		if ruleStatus != "" {
			if err := json.Unmarshal([]byte(ruleStatus), &rStatus); err != nil {
				log.Errorf("annotation rule status unmarshal failed %v", err)
				return err
			}
		}
		ag = deleteAllAzureRule(ag, groupName, rStatus)
	}
	_, err = c.AppGateway.CreateOrUpdate(context.TODO(), groupName, agName, *ag)
	if err != nil {
		log.Errorf("delete app gateway update application gateway error %v", err)
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
		log.Errorf("update backend pool ip update application gateway error %v", err)
		return err
	}

	return nil

}

func addAzureRule(c *client.Client, ag *network.ApplicationGateway, groupName, lbName, rule, hostname string) error {
	// add application gateway http listener
	listenerName := rule + "-cps-listener"
	portID := getFrontendPortID(ag)
	result := addAppGatewayHttpListener(ag, listenerName, hostname, portID)

	// add application gatway request routing rule
	ruleName := rule + "-cps-rule"
	poolName := lbName + "-backendpool"
	IDPrefix := strings.SplitAfter(portID, *ag.Name)[0]
	backendID := IDPrefix + "/backendAddressPools/" + poolName
	listenerID := IDPrefix + "/httpListeners/" + listenerName
	updated := addAppGatewayRequestRoutingRule(result, ruleName, backendID, listenerID)

	_, err := c.AppGateway.CreateOrUpdate(context.TODO(), groupName, *ag.Name, *updated)
	if err != nil {
		log.Errorf("add azure rule update application gateway error %v", err)
		return err
	}

	return nil
}

func getFrontendPortID(ag *network.ApplicationGateway) string {
	for _, port := range *ag.ApplicationGatewayPropertiesFormat.FrontendPorts {
		if *port.ApplicationGatewayFrontendPortPropertiesFormat.Port == 80 {
			return *port.ID
		}
	}
	return ""
}

func addAppGatewayHttpListener(ag *network.ApplicationGateway, listenerName, hostname, portID string) *network.ApplicationGateway {
	*ag.ApplicationGatewayPropertiesFormat.HTTPListeners = append((*ag.ApplicationGatewayPropertiesFormat.HTTPListeners)[:], network.ApplicationGatewayHTTPListener{
		Name: &listenerName,
		ApplicationGatewayHTTPListenerPropertiesFormat: &network.ApplicationGatewayHTTPListenerPropertiesFormat{
			Protocol: "Http",
			HostName: &hostname,
			FrontendIPConfiguration: &network.SubResource{
				ID: (*ag.ApplicationGatewayPropertiesFormat.FrontendIPConfigurations)[0].ID,
			},
			FrontendPort: &network.SubResource{
				ID: &portID,
			},
		},
	})

	return ag
}

func addAppGatewayRequestRoutingRule(ag *network.ApplicationGateway, ruleName, backendID, listenerID string) *network.ApplicationGateway {
	*ag.ApplicationGatewayPropertiesFormat.RequestRoutingRules = append((*ag.ApplicationGatewayPropertiesFormat.RequestRoutingRules)[:], network.ApplicationGatewayRequestRoutingRule{
		Name: &ruleName,
		ApplicationGatewayRequestRoutingRulePropertiesFormat: &network.ApplicationGatewayRequestRoutingRulePropertiesFormat{
			RuleType: "Basic",
			BackendAddressPool: &network.SubResource{
				ID: &backendID,
			},
			BackendHTTPSettings: &network.SubResource{
				ID: (*ag.ApplicationGatewayPropertiesFormat.BackendHTTPSettingsCollection)[0].ID,
			},
			HTTPListener: &network.SubResource{
				ID: &listenerID,
			},
		},
	})

	return ag
}

func deleteAllAzureRule(ag *network.ApplicationGateway, groupName string, rule map[string]string) *network.ApplicationGateway {
	for k, v := range rule {
		if v == "Success" {
			ruleName := k + "-cps-rule"
			listenerName := k + "-cps-listener"
			result := deleteAppGatewayRequestRoutingRule(ag, ruleName)

			// delete application gateway http listener
			ag = deleteAppGatewayHttpListener(result, listenerName)
		}
	}
	return ag
}

func addAllAzureRule(ag *network.ApplicationGateway, poolName string, rule map[string]string) *network.ApplicationGateway {
	for k, v := range rule {
		listenerName := k + "-cps-listener"
		portID := getFrontendPortID(ag)
		result := addAppGatewayHttpListener(ag, listenerName, v, portID)

		// add application gatway request routing rule
		ruleName := k + "-cps-rule"
		IDPrefix := strings.SplitAfter(portID, *ag.Name)[0]
		backendID := IDPrefix + "/backendAddressPools/" + poolName
		listenerID := IDPrefix + "/httpListeners/" + listenerName
		ag = addAppGatewayRequestRoutingRule(result, ruleName, backendID, listenerID)
	}
	return ag
}

func deleteAzureRule(c *client.Client, ag *network.ApplicationGateway, groupName, rule string) error {
	// delete application gatway request routing rule
	ruleName := rule + "-cps-rule"
	listenerName := rule + "-cps-listener"
	result := deleteAppGatewayRequestRoutingRule(ag, ruleName)

	// delete application gateway http listener
	updated := deleteAppGatewayHttpListener(result, listenerName)

	_, err := c.AppGateway.CreateOrUpdate(context.TODO(), groupName, *updated.Name, *updated)
	if err != nil {
		log.Errorf("update application gateway error %v", err)
		return err
	}

	return nil
}

func deleteAppGatewayHttpListener(ag *network.ApplicationGateway, listenerName string) *network.ApplicationGateway {
	var hl []network.ApplicationGatewayHTTPListener
	for _, listener := range *ag.ApplicationGatewayPropertiesFormat.HTTPListeners {
		if *listener.Name != listenerName {
			hl = append(hl, listener)
		}
	}
	*ag.ApplicationGatewayPropertiesFormat.HTTPListeners = hl

	return ag
}

func deleteAppGatewayRequestRoutingRule(ag *network.ApplicationGateway, ruleName string) *network.ApplicationGateway {
	var rr []network.ApplicationGatewayRequestRoutingRule
	for _, rule := range *ag.ApplicationGatewayPropertiesFormat.RequestRoutingRules {
		if *rule.Name != ruleName {
			rr = append(rr, rule)
		}
	}
	*ag.ApplicationGatewayPropertiesFormat.RequestRoutingRules = rr

	return ag
}
