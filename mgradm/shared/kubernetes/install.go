// SPDX-FileCopyrightText: 2024 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package kubernetes

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"github.com/uyuni-project/uyuni-tools/mgradm/shared/ssl"
	cmd_utils "github.com/uyuni-project/uyuni-tools/mgradm/shared/utils"
	"github.com/uyuni-project/uyuni-tools/shared"
	"github.com/uyuni-project/uyuni-tools/shared/kubernetes"
	. "github.com/uyuni-project/uyuni-tools/shared/l10n"
	"github.com/uyuni-project/uyuni-tools/shared/types"
	"github.com/uyuni-project/uyuni-tools/shared/utils"
)

// HELM_APP_NAME is the Helm application name.
const HELM_APP_NAME = "uyuni"

// Deploy execute a deploy of a given image and helm to a cluster.
func Deploy(
	cnx *shared.Connection,
	registry string,
	imageFlags *types.ImageFlags,
	hubXmlrpcFlags *cmd_utils.HubXmlrpcFlags,
	helmFlags *cmd_utils.HelmFlags,
	sslFlags *cmd_utils.SslCertFlags,
	clusterInfos *kubernetes.ClusterInfos,
	fqdn string,
	debug bool,
	prepare bool,
	helmArgs ...string,
) error {
	// If installing on k3s, install the traefik helm config in manifests
	isK3s := clusterInfos.IsK3s()
	IsRke2 := clusterInfos.IsRke2()
	if !prepare {
		tcpPorts, udpPorts := GetPortLists(hubXmlrpcFlags.Replicas > 0, debug)
		if isK3s {
			kubernetes.InstallK3sTraefikConfig(tcpPorts, udpPorts)
		} else if IsRke2 {
			kubernetes.InstallRke2NginxConfig(tcpPorts, udpPorts, helmFlags.Uyuni.Namespace)
		}
	}

	serverImage, err := utils.ComputeImage(registry, utils.DefaultTag, *imageFlags)
	if err != nil {
		return utils.Errorf(err, L("failed to compute image URL"))
	}

	hubXmlrpcImage, err := utils.ComputeImage(registry, imageFlags.Tag, hubXmlrpcFlags.Image)
	if err != nil {
		return utils.Errorf(err, L("failed to compute image URL"))
	}

	// Install the uyuni server helm chart
	err = UyuniUpgrade(serverImage, imageFlags.PullPolicy, hubXmlrpcFlags.Replicas, hubXmlrpcImage, helmFlags, clusterInfos.GetKubeconfig(), fqdn, clusterInfos.Ingress, helmArgs...)
	if err != nil {
		return utils.Errorf(err, L("cannot upgrade"))
	}

	// Wait for the pod to be started
	err = kubernetes.WaitForDeployment(helmFlags.Uyuni.Namespace, HELM_APP_NAME, "uyuni")
	if err != nil {
		return utils.Errorf(err, L("cannot deploy"))
	}
	return cnx.WaitForServer()
}

// DeployCertificate executre a deploy a new certificate given an helm.
func DeployCertificate(helmFlags *cmd_utils.HelmFlags, sslFlags *cmd_utils.SslCertFlags, rootCa string,
	ca *ssl.SslPair, kubeconfig string, fqdn string, imagePullPolicy string) ([]string, error) {
	helmArgs := []string{}
	if sslFlags.UseExisting() {
		if err := DeployExistingCertificate(helmFlags, sslFlags, kubeconfig); err != nil {
			return helmArgs, err
		}
	} else {
		// Install cert-manager and a self-signed issuer ready for use
		issuerArgs, err := installSslIssuers(helmFlags, sslFlags, rootCa, ca, kubeconfig, fqdn, imagePullPolicy)
		if err != nil {
			return []string{}, utils.Errorf(err, L("cannot install cert-manager and self-sign issuer"))
		}
		helmArgs = append(helmArgs, issuerArgs...)

		// Extract the CA cert into uyuni-ca config map as the container shouldn't have the CA secret
		extractCaCertToConfig(helmFlags.Uyuni.Namespace)
	}

	return helmArgs, nil
}

// DeployExistingCertificate execute a deploy of an existing certificate.
func DeployExistingCertificate(helmFlags *cmd_utils.HelmFlags, sslFlags *cmd_utils.SslCertFlags, kubeconfig string) error {
	// Deploy the SSL Certificate secret and CA configmap
	serverCrt, rootCaCrt := ssl.OrderCas(&sslFlags.Ca, &sslFlags.Server)
	serverKey := utils.ReadFile(sslFlags.Server.Key)
	if err := installTlsSecret(helmFlags.Uyuni.Namespace, serverCrt, serverKey, rootCaCrt); err != nil {
		return err
	}

	// Extract the CA cert into uyuni-ca config map as the container shouldn't have the CA secret
	extractCaCertToConfig(helmFlags.Uyuni.Namespace)
	return nil
}

// UyuniUpgrade runs an helm upgrade using images and helm configuration as parameters.
func UyuniUpgrade(serverImage string, pullPolicy string, hubXmlrpcReplicas int, hubXmlrpcImage string, helmFlags *cmd_utils.HelmFlags, kubeconfig string,
	fqdn string, ingress string, helmArgs ...string) error {
	log.Info().Msg(L("Installing Uyuni"))

	// The guessed ingress is passed before the user's value to let the user override it in case we got it wrong.
	helmParams := []string{
		"--set", "ingress=" + ingress,
	}

	extraValues := helmFlags.Uyuni.Values
	if extraValues != "" {
		helmParams = append(helmParams, "-f", extraValues)
	}

	// The values computed from the command line need to be last to override what could be in the extras
	helmParams = append(helmParams,
		"--set", "images.server="+serverImage,
		"--set", "pullPolicy="+kubernetes.GetPullPolicy(pullPolicy),
		"--set", "fqdn="+fqdn)

	if hubXmlrpcReplicas > 0 {
		log.Info().Msg(L("Enabling Hub XMLRPC API container."))
		helmParams = append(helmParams,
			"--set", fmt.Sprintf("hub.api.replicas=%v", hubXmlrpcReplicas),
			"--set", "images.hub_xmlrpc="+hubXmlrpcImage)
	}
	helmParams = append(helmParams, helmArgs...)

	namespace := helmFlags.Uyuni.Namespace
	chart := helmFlags.Uyuni.Chart
	version := helmFlags.Uyuni.Version
	return kubernetes.HelmUpgrade(kubeconfig, namespace, true, "", HELM_APP_NAME, chart, version, helmParams...)
}

// Upgrade will upgrade a server in a kubernetes cluster.
func Upgrade(
	globalFlags *types.GlobalFlags,
	image *types.ImageFlags,
	upgradeImage *types.ImageFlags,
	hubXmlrpcImage *types.ImageFlags,
	helm cmd_utils.HelmFlags,
	cmd *cobra.Command,
	args []string,
) error {
	for _, binary := range []string{"kubectl", "helm"} {
		if _, err := exec.LookPath(binary); err != nil {
			return fmt.Errorf(L("install %s before running this command"), binary)
		}
	}

	cnx := shared.NewConnection("kubectl", "", kubernetes.ServerFilter)
	namespace, err := cnx.GetNamespace("")
	if err != nil {
		return utils.Errorf(err, L("failed retrieving namespace"))
	}

	origHubXmlrpcImage, err := kubernetes.GetRunningImage("hub-xmlrpc-api")
	if err != nil {
		return utils.Errorf(err, L("failed to find Hub XML-RPC API container"))
	}

	serverImage, err := utils.ComputeImage(image.Registry, utils.DefaultTag, *image)
	if err != nil {
		return utils.Errorf(err, L("failed to compute image URL"))
	}

	inspectedValues, err := kubernetes.InspectKubernetes(namespace, serverImage, image.PullPolicy)
	if err != nil {
		return utils.Errorf(err, L("cannot inspect kubernetes values"))
	}

	err = cmd_utils.SanityCheck(cnx, inspectedValues, serverImage)
	if err != nil {
		return err
	}

	fqdn := inspectedValues.Fqdn
	if fqdn == "" {
		return fmt.Errorf(L("inspect function did non return fqdn value"))
	}

	clusterInfos, err := kubernetes.CheckCluster()
	if err != nil {
		return err
	}
	kubeconfig := clusterInfos.GetKubeconfig()

	scriptDir, err := utils.TempDir()
	if err != nil {
		return err
	}
	defer os.RemoveAll(scriptDir)

	//this is needed because folder with script needs to be mounted
	//check the node before scaling down
	nodeName, err := kubernetes.GetNode(namespace, kubernetes.ServerFilter)
	if err != nil {
		return utils.Errorf(err, L("cannot find node running uyuni"))
	}

	err = kubernetes.ReplicasTo(namespace, kubernetes.ServerApp, 0)
	if err != nil {
		return utils.Errorf(err, L("cannot set replica to 0"))
	}

	defer func() {
		// if something is running, we don't need to set replicas to 1
		if _, err = kubernetes.GetNode(namespace, kubernetes.ServerFilter); err != nil {
			err = kubernetes.ReplicasTo(namespace, kubernetes.ServerApp, 1)
		}
	}()
	if inspectedValues.ImagePgVersion > inspectedValues.CurrentPgVersion {
		log.Info().Msgf(L("Previous PostgreSQL is %[1]s, new one is %[2]s. Performing a DB version upgrade…"),
			inspectedValues.CurrentPgVersion, inspectedValues.ImagePgVersion)

		if err := RunPgsqlVersionUpgrade(image.Registry, *image, *upgradeImage, nodeName, namespace,
			inspectedValues.CurrentPgVersion, inspectedValues.ImagePgVersion,
		); err != nil {
			return utils.Errorf(err, L("cannot run PostgreSQL version upgrade script"))
		}
	} else if inspectedValues.ImagePgVersion == inspectedValues.CurrentPgVersion {
		log.Info().Msgf(L("Upgrading to %s without changing PostgreSQL version"), inspectedValues.UyuniRelease)
	} else {
		return fmt.Errorf(L("trying to downgrade PostgreSQL from %[1]s to %[2]s"),
			inspectedValues.CurrentPgVersion, inspectedValues.ImagePgVersion)
	}

	schemaUpdateRequired := inspectedValues.CurrentPgVersion != inspectedValues.ImagePgVersion
	if err := RunPgsqlFinalizeScript(serverImage, image.PullPolicy, namespace, nodeName, schemaUpdateRequired, false); err != nil {
		return utils.Errorf(err, L("cannot run PostgreSQL finalize script"))
	}

	if err := RunPostUpgradeScript(serverImage, image.PullPolicy, namespace, nodeName); err != nil {
		return utils.Errorf(err, L("cannot run post upgrade script"))
	}

	helmArgs := []string{}

	// Get the registry secret name if any
	pullSecret, err := kubernetes.GetDeploymentImagePullSecret(namespace, kubernetes.ServerFilter)
	if err != nil {
		return err
	}
	if pullSecret != "" {
		helmArgs = append(helmArgs, "--set", "registrySecret="+pullSecret)
	}

	hubXmlrpcImageName, err := utils.ComputeImage(image.Registry, image.Tag, *hubXmlrpcImage)
	if err != nil {
		return utils.Errorf(err, L("failed to compute image URL"))
	}

	hubXmlrpcReplicas := 0
	if origHubXmlrpcImage != "" {
		hubXmlrpcReplicas = 1
	}

	err = UyuniUpgrade(
		serverImage, image.PullPolicy, hubXmlrpcReplicas, hubXmlrpcImageName, &helm, kubeconfig, fqdn,
		clusterInfos.Ingress, helmArgs...,
	)
	if err != nil {
		return utils.Errorf(err, L("cannot upgrade to image %s"), serverImage)
	}

	return kubernetes.WaitForDeployment(namespace, "uyuni", "uyuni")
}
