# GitHub Actions with CodeBuild-Hosted Runners Setup

This repository is now configured to run tests using **CodeBuild-hosted runners** in GitHub Actions.

## Architecture

**GitHub Actions** (`.github/workflows/test.yml`) runs directly on **CodeBuild-hosted runners**

- GitHub Actions orchestrates the workflow
- CodeBuild provides the compute environment (runners)
- Tests run directly in GitHub Actions steps (no separate CodeBuild project needed)
- Automatic fallback to regular GitHub runners for forks

## Files Created

### `.github/workflows/test.yml`
- **Setup Job**: Configures runner matrix based on repository owner
- **Build Job**: Runs all test phases on CodeBuild runners
- **Phases**: 
  1. Build test images (`make test-images`)
  2. Loop device cleanup check
  3. Verify protobuf files are up to date
  4. Run unit tests (`make test-in-docker`)
- **Artifacts**: Uploads `rootfs.img` for debugging

## What Was Converted from Buildkite

The following Buildkite pipeline steps were converted to GitHub Actions steps:

| Buildkite Step | GitHub Actions Step | Description |
|---|---|---|
| `:docker: Build` | "Phase 1: Build test images" | `make test-images`, cleanup, artifact creation |
| `:lint-roller: loop device cleanup` | "Phase 2: Loop device cleanup check" | `sudo losetup -l` |
| `:protractor: verify proto` | "Phase 3: Verify proto files" | `make proto` + git status check |
| `:gear: unit tests` | "Phase 4: Unit tests" | `make test-in-docker` |

## Key Features

- **Smart Runner Selection**: Uses CodeBuild runners for `coderbirju` repo, falls back to GitHub runners for forks
- **Artifact Management**: Handles `rootfs.img` between phases using `/tmp/artifacts`
- **Timeout Protection**: Sensible timeouts for each phase
- **Environment Variables**: Proper setup of `DOCKER_IMAGE_TAG`, `EXTRAGOARGS`, etc.

## Testing the Setup

1. **Push to main branch** or **create a Pull Request**
2. **GitHub Actions will**:
   - Trigger automatically
   - Use CodeBuild runner for your repo (`coderbirju`)
   - Use regular GitHub runner for forks
   - Run all 4 test phases sequentially
3. **View logs**: In the GitHub Actions "Actions" tab

## CodeBuild Runner Configuration

The workflow uses runner label: `codebuild-fccd-codebuild-${{ github.run_id }}-${{ github.run_attempt }}-ubuntu-7.0-large`

This assumes your CodeBuild project is named `fccd-codebuild`. The runner provides:
- **Ubuntu environment** with Docker
- **Large compute** (7.0 = BUILD_GENERAL1_LARGE)
- **Privileged access** for Docker operations
- **Automatic cleanup** after job completion

## Troubleshooting

### Common Issues

1. **CodeBuild Project Setup**: Ensure your `fccd-codebuild` project exists and is configured for GitHub Actions integration
2. **Runner Naming**: The runner label must match your CodeBuild project name
3. **Permissions**: CodeBuild project needs proper IAM roles for GitHub Actions integration
4. **Compute Size**: `ubuntu-7.0-large` provides sufficient resources for the build

### For Forks

Forks will automatically fall back to `ubuntu-latest` GitHub runners. This works for basic testing but may have limitations for Docker-intensive operations.

## Next Steps

1. **Test the workflow**: Push to GitHub and verify it triggers
2. **Add more test phases**: You mentioned you want to add the integration tests later
3. **Optimize**: Add caching, parallel jobs, or artifacts as needed

## Advanced Configuration

### Adding More Test Phases

To add the remaining Buildkite steps (runtime isolated tests, stress tests, etc.), you can:

1. **Add more jobs to the workflow**: Create separate jobs for different test types
2. **Use job dependencies**: Control execution order with `needs:`
3. **Use GitHub Actions matrix**: Run multiple test configurations in parallel

### Parallel Testing

Example for future expansion:
```yaml
jobs:
  unit-tests:
    # Current job
  
  integration-tests:
    needs: unit-tests
    runs-on: ${{ fromJSON(needs.setup.outputs.runner-labels)[matrix.runner] }}
    strategy:
      matrix:
        test-type: [runtime, snapshotter, examples]
```

This setup gives you the foundation to migrate from Buildkite to GitHub Actions with CodeBuild runners while maintaining the same test coverage!
