# Changelog
## [2.0.0](https://github.com/microsoft/go-mssqldb/compare/go-mssqldb-v1.9.6...go-mssqldb-v2.0.0) (2026-03-12)


### ⚠ BREAKING CHANGES

* Add `Hidden()` method to `ProtocolParser` interface

### Features

* Add admin protocol for DAC ([#110](https://github.com/microsoft/go-mssqldb/issues/110)) ([327d048](https://github.com/microsoft/go-mssqldb/commit/327d04818fe83f59fe9669c5f7d7e48d88f41129))
* Add device code and Az CLI auth types to azuread ([#149](https://github.com/microsoft/go-mssqldb/issues/149)) ([77786ad](https://github.com/microsoft/go-mssqldb/commit/77786ad1f08f02ad281cbcdb0019c32f1ba5e118))
* add driver name and version to TDS packets ([12048ef](https://github.com/microsoft/go-mssqldb/commit/12048ef431d5a33c2f34157bfb56dacf12af60b0))
* Add NullableUniqueIdentifier type ([fe7c3d4](https://github.com/microsoft/go-mssqldb/commit/fe7c3d4cdbbf456ba8f5445420b86d41acce301b))
* Add tracing data to prelogin and login7 packets ([#228](https://github.com/microsoft/go-mssqldb/issues/228)) ([dad23d2](https://github.com/microsoft/go-mssqldb/commit/dad23d2a2b931673360a00d3810ceaea061fb4b0)), closes [#226](https://github.com/microsoft/go-mssqldb/issues/226)
* Allow krb5 config through environment variables ([#157](https://github.com/microsoft/go-mssqldb/issues/157)) ([2b177e8](https://github.com/microsoft/go-mssqldb/commit/2b177e839e240251de3ca28a3fe48d6edf6e90a2))
* Implement AKV key provider for always encrypted ([#148](https://github.com/microsoft/go-mssqldb/issues/148)) ([eb38425](https://github.com/microsoft/go-mssqldb/commit/eb3842574b5d72325022022421d1ac01735f6d64))
* implement automated versioning with Release Please ([#310](https://github.com/microsoft/go-mssqldb/issues/310)) ([f49ac9b](https://github.com/microsoft/go-mssqldb/commit/f49ac9bd260fdf6c8a71244cdaa6f949cc3b2d72))
* Implement change password during login ([#141](https://github.com/microsoft/go-mssqldb/issues/141)) ([6b69560](https://github.com/microsoft/go-mssqldb/commit/6b6956068faaea7ea45b183c8b77dd57e99d5dc8))
* Implement support for the latest Azure credential types in the azuread package ([#269](https://github.com/microsoft/go-mssqldb/issues/269)) ([696c95e](https://github.com/microsoft/go-mssqldb/commit/696c95e1460695790a303acb7728b16fb4a197da))
* Shopspring decimal support ([#287](https://github.com/microsoft/go-mssqldb/issues/287)) ([f1ed1e4](https://github.com/microsoft/go-mssqldb/commit/f1ed1e4966542f04b70f1c24196bbb6faa6d2767))
* support configuring custom time.Location for datetime encoding and decoding via DSN ([#260](https://github.com/microsoft/go-mssqldb/issues/260)) ([5356bbc](https://github.com/microsoft/go-mssqldb/commit/5356bbc959db3589118da0b7ea5ae3c1f80194ab))


### Bug Fixes

* 166: Azure SQL long query: driver: bad connection ([#327](https://github.com/microsoft/go-mssqldb/issues/327)) ([df1dd71](https://github.com/microsoft/go-mssqldb/commit/df1dd7185ab5a4b98222e0852c7f057fa00f6e1c))
* 218 ([b144cbb](https://github.com/microsoft/go-mssqldb/commit/b144cbbeb817c8fb1b9401afdf29ec5a5ce92bd2))
* 370: bulkcopy: slice bounds out of range ([#393](https://github.com/microsoft/go-mssqldb/issues/393)) ([5e78ff4](https://github.com/microsoft/go-mssqldb/commit/5e78ff4b1b75df6d0dc5457f264ca7a7556419ca))
* 672: output parameters in queries ([#673](https://github.com/microsoft/go-mssqldb/issues/673)) ([a78da2a](https://github.com/microsoft/go-mssqldb/commit/a78da2aabd81789ce2188494d95efcdf97d940a3))
* Added multisubnetfailover option, set to false to prevent issue [#158](https://github.com/microsoft/go-mssqldb/issues/158) ([#159](https://github.com/microsoft/go-mssqldb/issues/159)) ([c902b67](https://github.com/microsoft/go-mssqldb/commit/c902b674869e3f177b723cf2a5b1364c14efda47))
* apply guidConversion option in TestBulkcopy ([#255](https://github.com/microsoft/go-mssqldb/issues/255)) ([48e8e0a](https://github.com/microsoft/go-mssqldb/commit/48e8e0a04a11ea4b10b853051853e5f81982a61a))
* big datetime in bulkcopy ([ad4c7e0](https://github.com/microsoft/go-mssqldb/commit/ad4c7e0b5fa29e366e2759f57067c671cd819f07))
* **CharsetToUTF8:** use strings.Builder ([#154](https://github.com/microsoft/go-mssqldb/issues/154)) ([d3f844a](https://github.com/microsoft/go-mssqldb/commit/d3f844a9b6536f225650ce40f5de4060813af821))
* Connection not closed when database name is incorrect [#173](https://github.com/microsoft/go-mssqldb/issues/173) fix ([#224](https://github.com/microsoft/go-mssqldb/issues/224)) ([573423d](https://github.com/microsoft/go-mssqldb/commit/573423d6141d93d52df6cde94181aef76719e857))
* Flush pending TLS handshake packet before switching transport ([#280](https://github.com/microsoft/go-mssqldb/issues/280)) ([e1eb509](https://github.com/microsoft/go-mssqldb/commit/e1eb509db79d8f302a244e62d91e89ad4e902e71))
* Handle extended chars in SQL instance names ([#138](https://github.com/microsoft/go-mssqldb/issues/138)) ([7804e0c](https://github.com/microsoft/go-mssqldb/commit/7804e0c41e730a713cbd4a0b1038476f1093667d))
* handle sql.NullTime parameters ([#195](https://github.com/microsoft/go-mssqldb/issues/195)) ([a1c982b](https://github.com/microsoft/go-mssqldb/commit/a1c982b6ab950a41215384de1aa6a602f1a48fec))
* make the message queue model work correctly ([#277](https://github.com/microsoft/go-mssqldb/issues/277)) ([5a620f4](https://github.com/microsoft/go-mssqldb/commit/5a620f4b49097fc625c4b44151dd8a67c44eb281))
* mips and mipsel builds ([#124](https://github.com/microsoft/go-mssqldb/issues/124)) ([78ad891](https://github.com/microsoft/go-mssqldb/commit/78ad8916425360606131a06af121720ba87b2daf))
* **namedpipe:** Restrict package to supported platforms ([#298](https://github.com/microsoft/go-mssqldb/issues/298)) ([0997918](https://github.com/microsoft/go-mssqldb/commit/0997918dbde87895c3f856e30fe385f05a3b86b9))
* npipe dialer now returns immediately on pipe not found ([c30824f](https://github.com/microsoft/go-mssqldb/commit/c30824f9ea3c3cd4f1b39fe2138e41b23b750672))
* protocol version ([ff5b16d](https://github.com/microsoft/go-mssqldb/commit/ff5b16d91271e9f99f4760c2b892be7c218d996f))
* protocol version ([c789659](https://github.com/microsoft/go-mssqldb/commit/c789659d8770f30ee5d06168b22d7d61d68b40b5))
* replace broken AppVeyor badge with GitHub Actions badge ([#334](https://github.com/microsoft/go-mssqldb/issues/334)) ([d3429f5](https://github.com/microsoft/go-mssqldb/commit/d3429f5bb895bdb884ae39a43d87bb384aa5353a))
* sanitize credentials from connection string parsing errors ([#319](https://github.com/microsoft/go-mssqldb/issues/319)) ([93f5ef0](https://github.com/microsoft/go-mssqldb/commit/93f5ef0dd5f02c9094a22d0052fb95cd553ba971))
* **sharedmemory:** Restrict package to supported platforms ([#299](https://github.com/microsoft/go-mssqldb/issues/299)) ([0bbbaf2](https://github.com/microsoft/go-mssqldb/commit/0bbbaf2502c84730740733286a5068656bf4fc27))
* support nullable types for bulkcopy ([#192](https://github.com/microsoft/go-mssqldb/issues/192)) ([b3a8513](https://github.com/microsoft/go-mssqldb/commit/b3a8513a140c07225104e3dc5f5fffd7b74c2ea7))
* Support nullable types in Always Encrypted ([#179](https://github.com/microsoft/go-mssqldb/issues/179)) ([80b58f2](https://github.com/microsoft/go-mssqldb/commit/80b58f21b881e2d17f48987733c84e0c97ef61dc))
* update release-please action and config ([#322](https://github.com/microsoft/go-mssqldb/issues/322)) ([5718c41](https://github.com/microsoft/go-mssqldb/commit/5718c41883b77b66d70481f8d467b8a315f3e5e5))
* use proper scope for Entra auth ([#198](https://github.com/microsoft/go-mssqldb/issues/198)) ([3ed002a](https://github.com/microsoft/go-mssqldb/commit/3ed002a8a4707fd3cd441ac0a9e94b744ac64483))


### Performance Improvements

* do not copy columnStruct ([719c5b7](https://github.com/microsoft/go-mssqldb/commit/719c5b7d846a176ddef5c9266df72b337bf4ca06))
* do not copy columnStruct ([2c7902a](https://github.com/microsoft/go-mssqldb/commit/2c7902a31d0c57e85919c410278afeac8bfbdadd))
* use pointer receiver for columnStruct to avoid copy ([#316](https://github.com/microsoft/go-mssqldb/issues/316)) ([1c8ea5b](https://github.com/microsoft/go-mssqldb/commit/1c8ea5b8a43a24ee9d52be0727d9d37e25124432))

## 1.9.6

### Features

* Added new `serverCertificate` connection parameter for byte-for-byte certificate validation, matching Microsoft.Data.SqlClient behavior. This parameter skips hostname validation, chain validation, and expiry checks, only verifying that the server's certificate exactly matches the provided file. This is useful when the server's hostname doesn't match the certificate CN/SAN. (#304)
* The existing `certificate` parameter maintains backward compatibility with traditional X.509 chain validation including hostname checks, expiry validation, and chain-of-trust verification.
* `serverCertificate` cannot be used with `certificate` or `hostnameincertificate` parameters to prevent conflicting validation methods.

## 1.9.3

### Bug fixes

* Fix parsing of ADO connection strings with double-quoted values containing semicolons (#282)

## 1.9.2

### Bug fixes

* Fix race condition in message queue query model (#277)

## 1.9.1

### Bug fixes

* Fix bulk insert failure with datetime values near midnight due to day overflow (#271)
* Fix: apply guidConversion option in TestBulkcopy (#255)

### Features

* support configuring custom time.Location for datetime encoding and decoding via DSN (#260)
* Implement support for the latest Azure credential types in the azuread package (#269)

## 1.8.2

### Bug fixes

* Added "Pwd" as a recognized alias for "Password" in connection strings (#262)
* Updated `isProc` to detect more keywords

## 1.7.0

### Changed

* Changed always encrypted key provider error handling not to panic on failure

### Features

* Support DER certificates for server authentication (#152)

### Bug fixes

* Improved speed of CharsetToUTF8 (#154)

## 1.7.0

### Changed

* krb5 authenticator supports standard Kerberos environment variables for configuration

## 1.6.0

### Changed

* Go.mod updated to Go 1.17
* Azure SDK for Go dependencies updated

### Features

* Added `ActiveDirectoryAzCli` and `ActiveDirectoryDeviceCode` authentication types to `azuread` package
* Always Encrypted encryption and decryption with 2 hour key cache (#116)
* 'pfx', 'MSSQL_CERTIFICATE_STORE', and 'AZURE_KEY_VAULT' encryption key providers
* TDS8 can now be used for connections by setting encrypt="strict"

## 1.5.0

### Features

### Bug fixes

* Handle extended character in SQL instance names for browser lookup (#122)

## 1.4.0

### Features

* Adds UnmarshalJSON interface for UniqueIdentifier (#126)

### Bug fixes

* Fixes MarshalText prototype for UniqueIdentifier

## 1.2.0

### Features

* A connector's dialer can now be used to resolve DNS if the dialer implements the `HostDialer` interface

## 1.0.0

### Features

* `admin` protocol for dedicated administrator connections

### Changed

* Added `Hidden()` method to `ProtocolParser` interface

## 0.21.0

### Features

* Updated azidentity to 1.2.1, which adds in memory cache for managed credentials ([#90](https://github.com/microsoft/go-mssqldb/pull/90))

### Bug fixes

* Fixed uninitialized server name in TLS config ([#93](https://github.com/microsoft/go-mssqldb/issues/93))([#94](https://github.com/microsoft/go-mssqldb/pull/94))
* Fixed several kerberos authentication usages on Linux with new krb5 authentication provider. ([#65](https://github.com/microsoft/go-mssqldb/pull/65))

### Changed

* New kerberos authenticator implementation uses more explicit connection string parameters.

| Old          | New                |
|--------------|--------------------|
| krb5conffile | krb5-configfile    |
| krbcache     | krb5-credcachefile |
| keytabfile   | krb5-keytabfile    |
| realm        | krb5-realm         |

## 0.20.0

### Features

* Add driver version and name to TDS login packets
* Add `pipe` connection string parameter for named pipe dialer
* Expose network errors that occur during connection establishment. Now they are
wrapped, and can be detected by using errors.As/Is practise. This connection
errors can, and could even before, happen anytime the sql.DB doesn't have free
connection for executed query.

### Bug fixes

* Added checks while reading prelogin for invalid data ([#64](https://github.com/microsoft/go-mssqldb/issues/64))([86ecefd8b](https://github.com/microsoft/go-mssqldb/commit/86ecefd8b57683aeb5ad9328066ee73fbccd62f5))

* Fixed multi-protocol dialer path to avoid unneeded SQL Browser queries
