#!/usr/bin/env lucicfg

luci.project(
    name = "goma-server",

    buildbucket = "cr-buildbucket.appspot.com",
    logdog = "luci-logdog.appspot.com",
    milo = "luci-milo.appspot.com",
    swarming = "chromium-swarm.appspot.com",

    acls = [
        # This project is publicly readable.
        acl.entry(
            roles = [
                acl.BUILDBUCKET_READER,
                acl.LOGDOG_READER,
                acl.PROJECT_CONFIGS_READER,
                acl.SCHEDULER_READER,
            ],
            groups = "all",
        ),
        # Allow committers to use CQ and to force-trigger and stop CI builds.
        acl.entry(
            roles = [
                acl.SCHEDULER_OWNER,
                acl.CQ_COMMITTER,
            ],
            groups = "project-goma-server-tryjob-access"
        ),
        # Ability to launch CQ dry runs.
        acl.entry(
            roles = acl.CQ_DRY_RUNNER,
            groups = "project-chromium-tryjob-access"
        ),

        acl.entry(
            roles = acl.LOGDOG_WRITER,
            groups = "luci-logdog-chromium-writers"
        ),
    ],
)

luci.milo(logo = "https://storage.googleapis.com/chrome-infra-public/logo/goma-server-logo.png")

# Without this, luci-logdog.cfg won't be made.
luci.logdog()

luci.cq(status_host = "chromium-cq-status.appspot.com")

luci.bucket(
    name = "try",
    acls = [
        # Allow launching tryjobs directly (in addition to doing it through CQ).
        acl.entry(
            roles = acl.BUILDBUCKET_TRIGGERER,
            groups = "project-goma-server-tryjob-access",
        ),
    ]
)


# The Milo builder list with all pre-submit builders, referenced below.
luci.list_view(
    name = "Try Builders",
)

# The CQ group with all pre-submit builders, referenced below.
luci.cq_group(
    name = "Main CQ",
    watch = cq.refset("https://chromium-review.googlesource.com/infra/goma/server"),
)
luci.builder(
    name = "linux_rel",
    bucket = "try",
    service_account = "goma-server-try-builder@chops-service-accounts.iam.gserviceaccount.com",
    executable = luci.recipe(
        name = "goma_server",
        cipd_package = "infra/recipe_bundles/chromium.googlesource.com/chromium/tools/build",
    ),
    dimensions = {
        "pool": "luci.flex.try",
        "os": "Ubuntu-16.04",
        "cpu": "x86-64",
    },
)

# Add to the CQ.
luci.cq_tryjob_verifier(
    builder = "try/linux_rel",
    cq_group = "Main CQ",
)

# And also to the pre-submit builders list.
luci.list_view_entry(
    builder = "try/linux_rel",
    list_view = "Try Builders",
)
