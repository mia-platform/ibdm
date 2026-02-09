# IBDM

`ibdm` is used to export data from various supported systems, manipulate them to conform to
various ITDs and send the results to the Mia-Platform Catalog.

## Why `ibdm`?

`ibdm` is the acronym for the [International Berthing and Docking Mechanism] that is the
ESA implementation of the International Docking System Standard for allowing different spacecraft
docking systems to operate between them.

And this binary is something like that, its purpose is to allow the connect heterogeneous external
system with their data structure to the Mia-Platform Catalog with user predefined ITDs that
are targeted to their needs and can hide complexities that are not needed or surface certain
values to use them more easily.

## EndUser Documentation

If yu are here to know how to use `ibdm` in your system go to the
[`ibdm User Documentation`](./docs/README.md).

## Development Setup

If you want to contribute or develop `ibdm` you need the following programs installed on your
machine:

- golang 1.25+
- gnumake

We recommend to [install golang] following the official guide for your operating system or use a
version manager tool like [gvm], [asdf-vm] or [mise-en-place].

### Build the Binary

After you have clone or downloaded the source code on your machine you can run `make build` for
building the binary for your machine. The result of the build can be found in the `bin` folder
at the root of the repository.  
You can find the binary at `bin/<operating-system>/<architecture>/ibdm`.

### Running Tests

To run the test suite you can use the command `make test` or `make test-coverage` to also see
the test coverage of the various modules.

## Contributing

To contribute back to the project please follow the guidelines in [CONTRIBUTING.md](/CONTRIBUTING.md).

## License

`ibdm` is licensed under [AGPL-3.0-only](./LICENSE). For Commercial and other exceptions please read
[LICENSING.md](./LICENSING.md)

[International Berthing and Docking Mechanism]: https://en.wikipedia.org/wiki/International_Berthing_and_Docking_Mechanism
[install golang]: https://go.dev/doc/install
[gvm]: https://github.com/moovweb/gvm
[asdf-vm]: https://asdf-vm.com
[mise-en-place]: https://mise.jdx.dev
