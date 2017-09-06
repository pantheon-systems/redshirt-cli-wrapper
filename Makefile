APP := redshirt-cli-wrapper

include devops/make/common.mk
include devops/make/common-docker.mk
include devops/make/common-docs.mk
include devops/make/common-go.mk

# extend the update-makefiles task to remove files we don't need
update-makefiles::
	make prune-common-make

# strip out everything from common-makefiles that we don't want.
prune-common-make:
	@find devops/make -type f  \
		-not -name common.mk \
		-not -name common-docker.mk \
		-not -name common-docs.mk \
		-not -name common-go.mk \
		-not -name install-go.sh \
		-not -name install-gcloud.sh \
		-not -name install-shellcheck.sh \
		-not -name update-kube-object.sh \
		-delete
	@find devops/make -empty -delete
	@git add devops/make
	@git commit -C HEAD --amend
