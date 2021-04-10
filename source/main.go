package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/cfn"
	"github.com/aws/aws-lambda-go/lambda"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

func handleError(err error) {
	if err != nil {
		log.Println(err)
	}
}

func modifyVpn(ec2Svc *ec2.EC2, endpointID string, vpcID string, sgID string, subnetID string) (err error) {
	// Add the VPC and SG to the VPN endpoint
	modifyInput := &ec2.ModifyClientVpnEndpointInput{
		ClientVpnEndpointId: aws.String(endpointID),
		VpcId:               aws.String(vpcID),
		SecurityGroupIds:    []*string{&sgID},
	}
	_, err = ec2Svc.ModifyClientVpnEndpoint(modifyInput)
	handleError(err)
	if err != nil {
		return
	}

	// Associate the VPN with the subnet
	associateInput := &ec2.AssociateClientVpnTargetNetworkInput{
		ClientVpnEndpointId: aws.String(endpointID),
		SubnetId:            aws.String(subnetID),
	}
	_, err = ec2Svc.AssociateClientVpnTargetNetwork(associateInput)
	handleError(err)
	return
}

func waitForAssociation(ec2Svc *ec2.EC2, endpointID string) (err error) {
	describeInput := &ec2.DescribeClientVpnEndpointsInput{
		ClientVpnEndpointIds: []*string{&endpointID},
	}
	describeOutput, err := ec2Svc.DescribeClientVpnEndpoints(describeInput)
	if err != nil {
		return
	}
	if *describeOutput.ClientVpnEndpoints[0].Status.Code != ec2.ClientVpnEndpointStatusCodeAvailable {
		time.Sleep(3e10) // Wait for 30s before polling again
		err = waitForAssociation(ec2Svc, endpointID)
	}

	return
}

func disassociateVpn(ec2Svc *ec2.EC2, endpointID string) (err error) {
	targetNetworksInput := &ec2.DescribeClientVpnTargetNetworksInput{
		ClientVpnEndpointId: aws.String(endpointID),
	}
	targetNetworksOutput, err := ec2Svc.DescribeClientVpnTargetNetworks(targetNetworksInput)
	handleError(err)
	if err != nil {
		return
	}

	// Disassociate *all* networks from the VPN
	for _, net := range targetNetworksOutput.ClientVpnTargetNetworks {
		disassociationInput := &ec2.DisassociateClientVpnTargetNetworkInput{
			AssociationId:       aws.String(*net.AssociationId),
			ClientVpnEndpointId: aws.String(endpointID),
		}
		_, err = ec2Svc.DisassociateClientVpnTargetNetwork(disassociationInput)
		handleError(err)
		if err != nil {
			return
		}
	}

	return
}

func handler(ctx context.Context, event cfn.Event) (physicalResourceID string, data map[string]interface{}, err error) {
	endpointID, _ := event.ResourceProperties["EndpointId"].(string)
	vpcID, _ := event.ResourceProperties["VpcId"].(string)
	sgID, _ := event.ResourceProperties["SecurityGroupId"].(string)
	subnetID, _ := event.ResourceProperties["SubnetId"].(string)

	sess, _ := session.NewSession(&aws.Config{Region: aws.String(os.Getenv("AWS_REGION"))})
	ec2Svc := ec2.New(sess)

	switch string(event.RequestType) {
	case "Update":
		err = disassociateVpn(ec2Svc, endpointID)
		if err == nil {
			return
		}
		fallthrough
	case "Create":
		err = modifyVpn(ec2Svc, endpointID, vpcID, sgID, subnetID)
		if err != nil {
			err = waitForAssociation(ec2Svc, endpointID)
		}
	case "Delete":
		err = disassociateVpn(ec2Svc, endpointID)
		handleError(err)
	default:
		err = fmt.Errorf("unknown RequestType %s", string(event.RequestType))
		handleError(err)
	}

	if err == nil {
		physicalResourceID = endpointID
	}

	return
}

func main() {
	lambda.Start(cfn.LambdaWrap(handler))
}
