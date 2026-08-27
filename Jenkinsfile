// Jenkins pipeline for the voip-platform project.
//
// Requires on the agent: Docker (with socket access) and curl. Go and
// golangci-lint are NOT required on the agent — they're installed by mise
// from .mise.toml, so CI uses the exact same tool versions as local dev.
//
// Expected Jenkins configuration:
//   - Multibranch pipeline (so `branch 'main'` gating below works), or
//     adjust the `when` blocks to your branching model.
//   - Credentials:
//       docker-registry-creds  (username/password) - push access to REGISTRY
//       voip-deploy-ssh-key    (SSH private key)    - access to the deploy host
//   - Job/global environment variables (or edit the defaults below):
//       REGISTRY      e.g. registry.example.com/voip
//       DEPLOY_HOST   e.g. voip.example.com
//       DEPLOY_USER   e.g. deploy
//       DEPLOY_PATH   absolute path to this repo checkout on the deploy host

pipeline {
    agent { label 'docker' }

    options {
        timestamps()
        disableConcurrentBuilds()
        buildDiscarder(logRotator(numToKeepStr: '20'))
    }

    environment {
        MISE_BIN     = "${HOME}/.local/bin/mise"
        REGISTRY     = "${env.REGISTRY ?: 'registry.example.com/voip'}"
        IMAGE_TAG    = "${env.GIT_COMMIT.take(7)}"
        DEPLOY_HOST  = "${env.DEPLOY_HOST ?: ''}"
        DEPLOY_USER  = "${env.DEPLOY_USER ?: 'deploy'}"
        DEPLOY_PATH  = "${env.DEPLOY_PATH ?: '/opt/voip-platform'}"
    }

    stages {
        stage('Setup') {
            steps {
                sh '''
                    set -eu
                    if [ ! -x "$MISE_BIN" ]; then
                        curl -fsSL https://mise.run | sh
                    fi
                    "$MISE_BIN" trust
                    "$MISE_BIN" install
                '''
            }
        }

        stage('Lint') {
            steps {
                sh '"$MISE_BIN" x -- golangci-lint run ./...'
            }
        }

        stage('Test') {
            steps {
                sh '"$MISE_BIN" x -- go test ./... -race -cover'
            }
        }

        stage('Build images') {
            steps {
                sh """
                    docker build -t ${REGISTRY}/voip-app:${IMAGE_TAG} -f deploy/app/Dockerfile .
                    docker build -t ${REGISTRY}/voip-asterisk:${IMAGE_TAG} deploy/asterisk
                """
            }
        }

        stage('Push images') {
            when { branch 'main' }
            steps {
                withCredentials([usernamePassword(credentialsId: 'docker-registry-creds', usernameVariable: 'REG_USER', passwordVariable: 'REG_PASS')]) {
                    sh """
                        echo "\$REG_PASS" | docker login ${REGISTRY} -u "\$REG_USER" --password-stdin
                        docker tag ${REGISTRY}/voip-app:${IMAGE_TAG} ${REGISTRY}/voip-app:latest
                        docker tag ${REGISTRY}/voip-asterisk:${IMAGE_TAG} ${REGISTRY}/voip-asterisk:latest
                        docker push ${REGISTRY}/voip-app:${IMAGE_TAG}
                        docker push ${REGISTRY}/voip-app:latest
                        docker push ${REGISTRY}/voip-asterisk:${IMAGE_TAG}
                        docker push ${REGISTRY}/voip-asterisk:latest
                    """
                }
            }
        }

        stage('Deploy') {
            when { branch 'main' }
            steps {
                sshagent(credentials: ['voip-deploy-ssh-key']) {
                    sh """
                        ssh -o StrictHostKeyChecking=no ${DEPLOY_USER}@${DEPLOY_HOST} '
                            cd ${DEPLOY_PATH} &&
                            git pull --ff-only &&
                            REGISTRY=${REGISTRY} IMAGE_TAG=${IMAGE_TAG} docker compose -f deploy/docker-compose.prod.yml pull &&
                            REGISTRY=${REGISTRY} IMAGE_TAG=${IMAGE_TAG} docker compose -f deploy/docker-compose.prod.yml up -d
                        '
                    """
                }
            }
        }
    }

    post {
        always {
            sh 'docker image prune -f --filter "until=72h" || true'
            cleanWs()
        }
    }
}
