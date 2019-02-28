// Copyright 2015-2018 Amazon.com, Inc. or its affiliates. All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License"). You may
// not use this file except in compliance with the License. A copy of the
// License is located at
//
//	http://aws.amazon.com/apache2.0/
//
// or in the "license" file accompanying this file. This file is distributed
// on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either
// express or implied. See the License for the specific language governing
// permissions and limitations under the License.

package regcreds

import (
	"fmt"
	"time"

	iamClient "github.com/aws/amazon-ecs-cli/ecs-cli/modules/clients/aws/iam"
	kmsClient "github.com/aws/amazon-ecs-cli/ecs-cli/modules/clients/aws/kms"
	"github.com/aws/amazon-ecs-cli/ecs-cli/modules/utils/regcredio"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/iam"
	log "github.com/sirupsen/logrus"
)

const (
	assumeRolePolicyDocString = `{"Version":"2008-10-17","Statement":[{"Sid":"","Effect":"Allow","Principal":{"Service":"ecs-tasks.amazonaws.com"},"Action":"sts:AssumeRole"}]}`
	roleDescriptionString     = "Role generated by the ecs-cli"
)

type executionRoleParams struct {
	CredEntries map[string]regcredio.CredsOutputEntry
	RoleName    string
	Region      string
	Tags        map[string]*string
}

// returns the time of IAM policy creation so that other resources (i.e., output file) can be dated to match
func createTaskExecutionRole(params executionRoleParams, iamClient iamClient.Client, kmsClient kmsClient.Client) (*time.Time, error) {
	log.Infof("Creating resources for task execution role %s...", params.RoleName)

	// create role
	roleName, err := createOrFindRole(params.RoleName, iamClient, convertToIAMTags(params.Tags))
	if err != nil {
		return nil, err
	}

	// generate policy document
	policyDoc, err := generateSecretsPolicy(params.CredEntries, kmsClient)
	if err != nil {
		return nil, err
	}

	// create datetime for policy & output
	createTime := time.Now().UTC()

	// create the new policy
	newPolicy, err := createRegistryCredentialsPolicy(params.RoleName, policyDoc, createTime, iamClient)
	if err != nil {
		return nil, err
	}
	log.Infof("Created new task execution role policy %s", aws.StringValue(newPolicy.Arn))

	// attach managed execution role policy & new credentials policy to role
	err = attachRolePolicies(*newPolicy.Arn, roleName, params.Region, iamClient)
	if err != nil {
		return nil, err
	}

	return &createTime, nil
}

func createRegistryCredentialsPolicy(roleName, policyDoc string, createTime time.Time, client iamClient.Client) (*iam.Policy, error) {
	newPolicyName := generateECSResourceName(roleName + "-policy-" + createTime.Format(regcredio.ECSCredFileTimeFmt))
	policyDescriptionFmtString := "Policy generated by the ecs-cli for role: %s"

	createPolicyRequest := iam.CreatePolicyInput{
		PolicyName:     newPolicyName,
		PolicyDocument: aws.String(policyDoc),
		Description:    aws.String(fmt.Sprintf(policyDescriptionFmtString, roleName)),
	}

	policyResult, err := client.CreatePolicy(createPolicyRequest)
	if err != nil {
		return nil, err
	}
	return policyResult.Policy, nil
}

func createOrFindRole(roleName string, client iamClient.Client, tags []*iam.Tag) (string, error) {
	roleResult, err := client.CreateOrFindRole(roleName, roleDescriptionString, assumeRolePolicyDocString, tags)
	if err != nil {
		return "", err
	}

	if roleResult != "" {
		log.Infof("Created new task execution role %s", roleResult)
	} else {
		log.Infof("Using existing role %s", roleName)
	}

	return roleName, nil
}

func attachRolePolicies(secretPolicyARN, roleName, region string, client iamClient.Client) error {
	managedPolicyARN := getExecutionRolePolicyARN(region)
	_, err := client.AttachRolePolicy(managedPolicyARN, roleName)
	if err != nil {
		return err
	}
	log.Infof("Attached AWS managed policy %s to role %s", managedPolicyARN, roleName)

	_, err = client.AttachRolePolicy(secretPolicyARN, roleName)
	if err != nil {
		return err
	}
	log.Infof("Attached new policy %s to role %s", secretPolicyARN, roleName)

	return nil
}

func convertToIAMTags(tags map[string]*string) []*iam.Tag {
	var iamTags []*iam.Tag
	for key, value := range tags {
		iamTags = append(iamTags, &iam.Tag{
			Key:   aws.String(key),
			Value: value,
		})
	}

	return iamTags
}
