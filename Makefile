all: docker

.PHONY: docker
docker:
	docker build -t pump-autoswitch:latest .
	docker image tag pump-autoswitch:latest sarah.fritz.box:5000/pump-autoswitch:latest
	docker image push sarah.fritz.box:5000/pump-autoswitch
