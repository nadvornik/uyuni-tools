// SPDX-FileCopyrightText: 2024 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package status

import (
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	adm_utils "github.com/uyuni-project/uyuni-tools/mgradm/shared/utils"
	"github.com/uyuni-project/uyuni-tools/shared"
	. "github.com/uyuni-project/uyuni-tools/shared/l10n"
	"github.com/uyuni-project/uyuni-tools/shared/podman"
	"github.com/uyuni-project/uyuni-tools/shared/types"
	"github.com/uyuni-project/uyuni-tools/shared/utils"
)

func podmanStatus(
	globalFlags *types.GlobalFlags,
	flags *statusFlags,
	cmd *cobra.Command,
	args []string,
) error {
	// Show the status and that's it if the service is not running
	if !podman.IsServiceRunning(podman.ServerService) {
		if err := utils.RunCmdStdMapping(zerolog.DebugLevel, "systemctl", "status", "--no-pager", podman.ServerService); err != nil {
			return utils.Errorf(err, L("failed to get status of the server service"))
		}
		return nil
	}

	// Run spacewalk-service status in the container
	cnx := shared.NewConnection("podman", podman.ServerContainerName, "")
	if err := adm_utils.ExecCommand(zerolog.InfoLevel, cnx, "spacewalk-service", "status"); err != nil {
		return utils.Errorf(err, L("failed to run spacewalk-service status"))
	}

	if podman.HasService(podman.ServerAttestationService) {
		println() // add an empty line between the previous logs and this one
		if err := utils.RunCmdStdMapping(zerolog.DebugLevel, "systemctl", "status", "--no-pager", podman.ServerAttestationService); err != nil {
			return utils.Errorf(err, L("failed to get status of the server service"))
		}
		return nil
	}

	return nil
}
