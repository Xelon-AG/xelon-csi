# Xelon Persistent Storage CSI Driver


## Overview

The Container Storage Interface [(CSI)](https://github.com/container-storage-interface/spec) Driver for Xelon Persistent
Storage enables container orchestrators such as Kubernetes to manage the life-cycle of persistent storage claims.

More information about the Kubernetes CSI can be found in the GitHub [Kubernetes CSI](https://kubernetes-csi.github.io/docs/example.html)
and [CSI Spec](https://github.com/container-storage-interface/spec/) repos.


## Compatibility

The deprecated legacy v0 driver has been removed. Deployments must not pass the removed `--use-legacy-driver`,
`--log-level`, or `--metadata-file` flags.


## Contributing

We hope you'll get involved! Read our [Contributors' Guide](.github/CONTRIBUTING.md) for details.
