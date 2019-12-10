pipeline {
  agent {
    kubernetes {
      label 'metallb'
      defaultContainer 'jnlp'
      yamlFile 'JenkinsPod.yaml'
    }
  }

  environment {
    DOCKER_REGISTRY = 'gcr.io'
    ORG         = 'nks-images'
    APP_NAME    = 'metallb'
    CONTROLLER  = 'controller'
    SPEAKER     = 'speaker'
    CONTROLLER_REPOSITORY = "${ORG}/${APP_NAME}/${CONTROLLER}"
    SPEAKER_REPOSITORY = "${ORG}/${APP_NAME}/${SPEAKER}"
    GO111MODULE = 'on'
    GOPATH = "${WORKSPACE}/go"
    GITHUB_TOKEN = credentials('github-token-jenkins')
  }

  stages {

    stage('build') {
      when {
        anyOf {
          branch 'master'
          branch 'PR-*'
        }
      }
      steps {
        container('builder-base') {
          // We need to provide a personal access token to fetch private dependencies
          sh("go build -v -o build/amd64/controller/controller -ldflags '-X go.universe.tf/metallb/internal/version.gitCommit=6ea9bc6e-dirty -X go.universe.tf/metallb/internal/version.gitBranch=task/introduce-dynamic-addresses' go.universe.tf/metallb/controller")
          script {
            image = docker.build("${CONTROLLER_REPOSITORY}", "--build-arg GITHUB_TOKEN=${GITHUB_TOKEN} .")
          }
        }
      }
    }

    stage('publish: dev') {
      when {
        branch 'PR-*'
      }
      environment {
        GIT_COMMIT_SHORT = sh(
                script: "printf \$(git rev-parse --short ${GIT_COMMIT})",
                returnStdout: true
        ).trim()
      }
      steps {
        container('builder-base') {
          script {
            docker.withRegistry("https://${DOCKER_REGISTRY}", "gcr:${ORG}") {
              image.push("dev-${GIT_COMMIT_SHORT}")
              image.push("dev")
            }
          }
        }
      }
    }

    stage('publish: master') {
      when {
        branch 'master'
      }
      environment {
        GIT_COMMIT_SHORT = sh(
                script: "printf \$(git rev-parse --short ${GIT_COMMIT})",
                returnStdout: true
        ).trim()
      }
      steps {
        container('builder-base') {
          script {
            docker.withRegistry("https://${DOCKER_REGISTRY}", "gcr:${ORG}") {
              image.push("${API_VERSION}-${GIT_COMMIT_SHORT}")
              image.push("${API_VERSION}")
            }
          }
        }
      }
    }

  }
}