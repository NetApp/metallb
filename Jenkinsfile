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
    stage('test') {
      steps {
        container('golang') {
          // We need to provide a personal access token to fetch private dependencies
          sh 'git config --global url."https://oauth2:${GITHUB_TOKEN}@github.com".insteadOf "https://github.com"'
          sh("make test")
        }
      }
    }

    stage('build') {
      when {
        anyOf {
          branch 'main'
          branch 'PR-*'
        }
      }
      environment {
        GIT_COMMIT_SHORT = sh(
                script: "printf \$(git rev-parse --short ${GIT_COMMIT})",
                returnStdout: true
        ).trim()
      }
      steps {
        container('golang') {
          sh("make build")
        }
        container('builder-base') {
          sh("docker build -t ${DOCKER_REGISTRY}/${CONTROLLER_REPOSITORY} -f ${CONTROLLER}/Dockerfile build/amd64/${CONTROLLER}")
          sh("docker build -t ${DOCKER_REGISTRY}/${SPEAKER_REPOSITORY} -f ${SPEAKER}/Dockerfile build/amd64/${SPEAKER}")
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
              sh 'docker tag ${DOCKER_REGISTRY}/${CONTROLLER_REPOSITORY} ${DOCKER_REGISTRY}/${CONTROLLER_REPOSITORY}:${GIT_COMMIT_SHORT}-dev'
              sh 'docker push ${DOCKER_REGISTRY}/${CONTROLLER_REPOSITORY}:${GIT_COMMIT_SHORT}-dev'
              sh 'docker tag ${DOCKER_REGISTRY}/${SPEAKER_REPOSITORY} ${DOCKER_REGISTRY}/${SPEAKER_REPOSITORY}:${GIT_COMMIT_SHORT}-dev'
              sh 'docker push ${DOCKER_REGISTRY}/${SPEAKER_REPOSITORY}:${GIT_COMMIT_SHORT}-dev'
            }
          }
        }
      }
    }

    stage('publish: main') {
      when {
        branch 'main'
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
              sh 'docker tag ${DOCKER_REGISTRY}/${CONTROLLER_REPOSITORY} ${DOCKER_REGISTRY}/${CONTROLLER_REPOSITORY}:${GIT_COMMIT_SHORT}'
              sh 'docker push ${DOCKER_REGISTRY}/${CONTROLLER_REPOSITORY}:${GIT_COMMIT_SHORT}'
              sh 'docker tag ${DOCKER_REGISTRY}/${SPEAKER_REPOSITORY} ${DOCKER_REGISTRY}/${SPEAKER_REPOSITORY}:${GIT_COMMIT_SHORT}'
              sh 'docker push ${DOCKER_REGISTRY}/${SPEAKER_REPOSITORY}:${GIT_COMMIT_SHORT}'
            }
          }
        }
      }
    }

  }
}