// SPDX-FileCopyrightText: 2023 SUSE LLC
//
// SPDX-License-Identifier: Apache-2.0

package templates

import (
	"io"
	"text/template"
)

const postgresVersionMigrationScriptTemplate = `#!/bin/bash
set -e
echo "Postgres version migration"

OLD_VERSION={{ .OldVersion }}
NEW_VERSION={{ .NewVersion }}
FAST_UPGRADE=--link

echo "Testing presence of postgresql$NEW_VERSION..."
test -d /usr/lib/postgresql$NEW_VERSION/bin
echo "Testing presence of postgresql$OLD_VERSION..."
test -d /usr/lib/postgresql$OLD_VERSION/bin

echo "Create a backup at /var/lib/pgsql/data-pg$OLD_VERSION..."
mv /var/lib/pgsql/data /var/lib/pgsql/data-pg$OLD_VERSION
echo "Create new database directory..."
mkdir /var/lib/pgsql/data
chown postgres:postgres /var/lib/pgsql/data

echo "Initialize new postgresql $NEW_VERSION database..."
. /etc/sysconfig/postgresql 2>/dev/null # Load locale for SUSE
PGHOME=$(getent passwd postgres | awk -F: '{print $6}')
#. $PGHOME/.i18n 2>/dev/null # Load locale for Enterprise Linux
if [ -z $POSTGRES_LANG ]; then
    POSTGRES_LANG="en_US.UTF-8"
    [ ! -z $LC_CTYPE ] && POSTGRES_LANG=$LC_CTYPE
fi

echo "Running initdb using postgres user"
echo "Any suggested command from the console should be run using postgres user"
su -s /bin/bash - postgres -c "initdb -D /var/lib/pgsql/data --locale=$POSTGRES_LANG"
echo "Successfully initialized new postgresql $NEW_VERSION database."
su -s /bin/bash - postgres -c "pg_upgrade --old-bindir=/usr/lib/postgresql$OLD_VERSION/bin --new-bindir=/usr/lib/postgresql$NEW_VERSION/bin --old-datadir=/var/lib/pgsql/data-pg$OLD_VERSION --new-datadir=/var/lib/pgsql/data $FAST_UPGRADE"

echo "DONE"`

type MigratePostgresVersionTemplateData struct {
	OldVersion string
	NewVersion string
	Kubernetes bool
}

func (data MigratePostgresVersionTemplateData) Render(wr io.Writer) error {
	t := template.Must(template.New("script").Parse(postgresVersionMigrationScriptTemplate))
	return t.Execute(wr, data)
}
