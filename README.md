# Graviton

![Graviton Graphic](graviton.png)

> The creator of light - forged in the limitless void

Graviton is a simple migration tool. Create migration files in typescript and
use the up command to apply them.

Graviton supports one or more databases for the same project. It currently
supports MongoDB, but will support other databases in future.

## Best Practices

Here are a few things to keep in mind when writing migrations.

- Try to keep migrations focused on a specific task or resource. This helps
keep migrations from getting too complex and reduces the risk that something
will go wrong while a specific migration is running. By splitting up migrations
in to smaller chunks, if something goes wrong during a migration, it's easier to
correct the issue and try again.
- Avoid writing migrations that depend on the state of an specific environment.
In future environment variables will be made available to migrations. It will be
recommended to use them to control environment specific migration logic.
- Migrations should not depend on state that does not exist in a previous
migration. If you write migrations that depend on data added outside of
previous migrations, it will not be possible to run the migrations on new
environments without manual intervention.
- Ensure migrations provide a functional down function, and that it reverses
the exact changes made by our up function. If you migration cannot be reversed
for good reasons, then do not include a down function at all in your migration.
Graviton will indicate that the migration cannot be reversed.

## Configuration

Graviton uses a configuration file to indicate which databases

## Commands

Use the following commands to manage your project's migration state.

### Up

```sh
graviton up [migration-name]
```

Up will apply pending migrations, one after the other in order of creation.
Up can optionally take a target migration name and will apply up to and including
the named migration.

The status command can be used to see what migrations are available to apply.

### Down

```sh
graviton down <migration-name>
```

Down will rollback applied migrations. A target migration name must be provided.
Down will rollback to and including the named migration.

The status command can be used to see what migrations are available to apply.

### Status

```sh
graviton status
```

Status will list all migrations that have been applied and pending. Use it
to see the migration state of your project.

### Set Head

```sh
graviton set-head <migration-name>
```

Set head will change which migration are marked as applied vs pending. Note that
it does not actually apply or rollback any migrations, it just changes what
migrations are recorded as applied.

This command is most useful when testing migrations, or skipping migrations
that may have been manually applied.

Note that if you'd like to set the head prior to the first applied migration,
use `-` as the migration name.
