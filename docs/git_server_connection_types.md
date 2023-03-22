# Different ways to connect to a Git repository
These are currently supported ways to connect to a Git server using channel and subscription.

## Connecting to a private repository using user and access token

1. Create a secret in the same namespace as channel. Set the `user` field to be a Git user ID and the `accessToken` field to be a Git personal access token. The values should be base64 encoded.

```
apiVersion: v1
kind: Secret
metadata:
  name: my-git-secret
  namespace: channel-ns
data:
  user: dXNlcgo=
  accessToken: cGFzc3dvcmQK
```

2. Configure the channel with this secret like this.

```
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: sample-channel
  namespace: channel-ns
spec:
    type: Git
    pathname: <Git URL>
    secretRef:
      name: my-git-secret
``` 

## Insecure HTTPS connection

You can use this connection method in development environment to connect to a privately hosted Git server with SSL certificates signed by custom or self-signed certificate authority. This is not recommented for production.

Specify `insecureSkipVerify: true` in the channel spec. Otherwise, the connection to the Git server will fail with an error similar to the following.

```
x509: certificate is valid for localhost.com, not localhost
```

```
apiVersion: apps.open-cluster-management.io/v1
ind: Channel
metadata:
labels:
  name: sample-channel
  namespace: sample
spec:
  type: GitHub
  pathname: <Git URL>
  insecureSkipVerify: true
```

## Using custom CA certificates for secure HTTPS connection

You can use this connection method to securely connect to a privately hosted Git server with SSL certificates signed by custom or self-signed certificate authority. 

1. Create a config map to contain the Git server's root and intermediate CA certificates in PEM format. The config map must be in the same namespace as the channel CR and should look like this. The field name must be `caCerts` and use `|`. `caCerts` can contain multiple certificates as shown below.

```
apiVersion: v1
kind: ConfigMap
metadata:
  name: git-ca
  namespace: channel-ns
data:
  caCerts: |
    # Git server root CA

    -----BEGIN CERTIFICATE-----
    MIIF5DCCA8wCCQDInYMol7LSDTANBgkqhkiG9w0BAQsFADCBszELMAkGA1UEBhMC
    Q0ExCzAJBgNVBAgMAk9OMRAwDgYDVQQHDAdUb3JvbnRvMQ8wDQYDVQQKDAZSZWRI
    YXQxDDAKBgNVBAsMA0FDTTFFMEMGA1UEAww8Z29ncy1zdmMtZGVmYXVsdC5hcHBz
    LnJqdW5nLWh1YjEzLmRldjA2LnJlZC1jaGVzdGVyZmllbGQuY29tMR8wHQYJKoZI
    hvcNAQkBFhByb2tlakByZWRoYXQuY29tMB4XDTIwMTIwMzE4NTMxMloXDTIzMDky
    MzE4NTMxMlowgbMxCzAJBgNVBAYTAkNBMQswCQYDVQQIDAJPTjEQMA4GA1UEBwwH
    VG9yb250bzEPMA0GA1UECgwGUmVkSGF0MQwwCgYDVQQLDANBQ00xRTBDBgNVBAMM
    PGdvZ3Mtc3ZjLWRlZmF1bHQuYXBwcy5yanVuZy1odWIxMy5kZXYwNi5yZWQtY2hl
    c3RlcmZpZWxkLmNvbTEfMB0GCSqGSIb3DQEJARYQcm9rZWpAcmVkaGF0LmNvbTCC
    AiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAM3nPK4mOQzaDAo6S3ZJ0Ic3
    U9p/NLodnoTIC+cn0q8qNCAjf13zbGB3bfN9Zxl8Q5fv+wYwHrUOReCp6U/InyQy
    6OS3gj738F635inz1KdyhKtlWW2p9Ye9DUtx1IlfHkDVdXtynjHQbsFNIdRHcpQP
    upM5pwPC3BZXqvXChhlfAy2m4yu7vy0hO/oTzWIwNsoL5xt0Lw4mSyhlEip/t8lU
    xn2y8qhm7MiIUpXuwWhSYgCrEVqmTcB70Pc2YRZdSFolMN9Et70MjQN0TXjoktH8
    PyASJIKIRd+48yROIbUn8rj4aYYBsJuoSCjJNwujZPbqseqUr42+v+Qp2bBj1Sjw
    +SEZfHTvSv8AqX0T6eo6njr578+DgYlwsS1A1zcAdzp8qmDGqvJDzwcnQVFmvaoM
    gGHCdJihfy3vDhxuZRDse0V4Pz6tl6iklM+tHrJL/bdL0NdfJXNCqn2nKrM51fpw
    diNXs4Zn3QSStC2x2hKnK+Q1rwCSEg/lBawgxGUslTboFH77a+Kwu4Oug9ibtm5z
    ISs/JY4Kiy4C2XJOltOR2XZYkdKaX4x3ctbrGaD8Bj+QHiSAxaaSXIX+VbzkHF2N
    aD5ijFUopjQEKFrYh3O93DB/URIQ+wHVa6+Kvu3uqE0cg6pQsLpbFVQ/I8xHvt9L
    kYy6z6V/nj9ZYKQbq/kPAgMBAAEwDQYJKoZIhvcNAQELBQADggIBAKZuc+lewYAv
    jaaSeRDRoToTb/yN0Xsi69UfK0aBdvhCa7/0rPHcv8hmUBH3YgkZ+CSA5ygajtL4
    g2E8CwIO9ZjZ6l+pHCuqmNYoX1wdjaaDXlpwk8hGTSgy1LsOoYrC5ZysCi9Jilu9
    PQVGs/vehQRqLV9uZBigG6oZqdUqEimaLHrOcEAHB5RVcnFurz0qNbT+UySjsD63
    9yJdCeQbeKAR9SC4hG13EbM/RZh0lgFupkmGts7QYULzT+oA0cCJpPLQl6m6qGyE
    kh9aBB7FLykK1TeXVuANlNU4EMyJ/e+uhNkS9ubNJ3vuRuo+ECHsha058yi16JC9
    NkZqP+df4Hp85sd+xhrgYieq7QGX2KOXAjqAWo9htoBhOyW3mm783A7WcOiBMQv0
    2UGZxMsRjlP6UqB08LsV5ZBAefElR344sokJR1de/Sx2J9J/am7yOoqbtKpQotIA
    XSUkATuuQw4ctyZLDkUpzrDzgd2Bt+aawF6sD2YqycaGFwv2YD9t1YlD6F4Wh8Mc
    20Qu5EGrkQTCWZ9pOHNSa7YQdmJzwbxJC4hqBpBRAJFI2fAIqFtyum6/8ZN9nZ9K
    FSEKdlu+xeb6Y6xYt0mJJWF6mCRi4i7IL74EU/VNXwFmfP6IadliUOST3w5t92cB
    M26t73UCExXMXTCQvnp0ki84PeR1kRk4
    -----END CERTIFICATE-----

    # Git server intermediate CA 1

    -----BEGIN CERTIFICATE-----
    MIIF5DCCA8wCCQDInYMol7LSDTANBgkqhkiG9w0BAQsFADCBszELMAkGA1UEBhMC
    Q0ExCzAJBgNVBAgMAk9OMRAwDgYDVQQHDAdUb3JvbnRvMQ8wDQYDVQQKDAZSZWRI
    YXQxDDAKBgNVBAsMA0FDTTFFMEMGA1UEAww8Z29ncy1zdmMtZGVmYXVsdC5hcHBz
    LnJqdW5nLWh1YjEzLmRldjA2LnJlZC1jaGVzdGVyZmllbGQuY29tMR8wHQYJKoZI
    hvcNAQkBFhByb2tlakByZWRoYXQuY29tMB4XDTIwMTIwMzE4NTMxMloXDTIzMDky
    MzE4NTMxMlowgbMxCzAJBgNVBAYTAkNBMQswCQYDVQQIDAJPTjEQMA4GA1UEBwwH
    VG9yb250bzEPMA0GA1UECgwGUmVkSGF0MQwwCgYDVQQLDANBQ00xRTBDBgNVBAMM
    PGdvZ3Mtc3ZjLWRlZmF1bHQuYXBwcy5yanVuZy1odWIxMy5kZXYwNi5yZWQtY2hl
    c3RlcmZpZWxkLmNvbTEfMB0GCSqGSIb3DQEJARYQcm9rZWpAcmVkaGF0LmNvbTCC
    AiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAM3nPK4mOQzaDAo6S3ZJ0Ic3
    U9p/NLodnoTIC+cn0q8qNCAjf13zbGB3bfN9Zxl8Q5fv+wYwHrUOReCp6U/InyQy
    6OS3gj738F635inz1KdyhKtlWW2p9Ye9DUtx1IlfHkDVdXtynjHQbsFNIdRHcpQP
    upM5pwPC3BZXqvXChhlfAy2m4yu7vy0hO/oTzWIwNsoL5xt0Lw4mSyhlEip/t8lU
    xn2y8qhm7MiIUpXuwWhSYgCrEVqmTcB70Pc2YRZdSFolMN9Et70MjQN0TXjoktH8
    PyASJIKIRd+48yROIbUn8rj4aYYBsJuoSCjJNwujZPbqseqUr42+v+Qp2bBj1Sjw
    +SEZfHTvSv8AqX0T6eo6njr578+DgYlwsS1A1zcAdzp8qmDGqvJDzwcnQVFmvaoM
    gGHCdJihfy3vDhxuZRDse0V4Pz6tl6iklM+tHrJL/bdL0NdfJXNCqn2nKrM51fpw
    diNXs4Zn3QSStC2x2hKnK+Q1rwCSEg/lBawgxGUslTboFH77a+Kwu4Oug9ibtm5z
    ISs/JY4Kiy4C2XJOltOR2XZYkdKaX4x3ctbrGaD8Bj+QHiSAxaaSXIX+VbzkHF2N
    aD5ijFUopjQEKFrYh3O93DB/URIQ+wHVa6+Kvu3uqE0cg6pQsLpbFVQ/I8xHvt9L
    kYy6z6V/nj9ZYKQbq/kPAgMBAAEwDQYJKoZIhvcNAQELBQADggIBAKZuc+lewYAv
    jaaSeRDRoToTb/yN0Xsi69UfK0aBdvhCa7/0rPHcv8hmUBH3YgkZ+CSA5ygajtL4
    g2E8CwIO9ZjZ6l+pHCuqmNYoX1wdjaaDXlpwk8hGTSgy1LsOoYrC5ZysCi9Jilu9
    PQVGs/vehQRqLV9uZBigG6oZqdUqEimaLHrOcEAHB5RVcnFurz0qNbT+UySjsD63
    9yJdCeQbeKAR9SC4hG13EbM/RZh0lgFupkmGts7QYULzT+oA0cCJpPLQl6m6qGyE
    kh9aBB7FLykK1TeXVuANlNU4EMyJ/e+uhNkS9ubNJ3vuRuo+ECHsha058yi16JC9
    NkZqP+df4Hp85sd+xhrgYieq7QGX2KOXAjqAWo9htoBhOyW3mm783A7WcOiBMQv0
    2UGZxMsRjlP6UqB08LsV5ZBAefElR344sokJR1de/Sx2J9J/am7yOoqbtKpQotIA
    XSUkATuuQw4ctyZLDkUpzrDzgd2Bt+aawF6sD2YqycaGFwv2YD9t1YlD6F4Wh8Mc
    20Qu5EGrkQTCWZ9pOHNSa7YQdmJzwbxJC4hqBpBRAJFI2fAIqFtyum6/8ZN9nZ9K
    FSEKdlu+xeb6Y6xYt0mJJWF6mCRi4i7IL74EU/VNXwFmfP6IadliUOST3w5t92cB
    M26t73UCExXMXTCQvnp0ki84PeR1kRk4
    -----END CERTIFICATE-----

    # Git server intermediate CA 2

    -----BEGIN CERTIFICATE-----
    MIIF5DCCA8wCCQDInYMol7LSDTANBgkqhkiG9w0BAQsFADCBszELMAkGA1UEBhMC
    Q0ExCzAJBgNVBAgMAk9OMRAwDgYDVQQHDAdUb3JvbnRvMQ8wDQYDVQQKDAZSZWRI
    YXQxDDAKBgNVBAsMA0FDTTFFMEMGA1UEAww8Z29ncy1zdmMtZGVmYXVsdC5hcHBz
    LnJqdW5nLWh1YjEzLmRldjA2LnJlZC1jaGVzdGVyZmllbGQuY29tMR8wHQYJKoZI
    hvcNAQkBFhByb2tlakByZWRoYXQuY29tMB4XDTIwMTIwMzE4NTMxMloXDTIzMDky
    MzE4NTMxMlowgbMxCzAJBgNVBAYTAkNBMQswCQYDVQQIDAJPTjEQMA4GA1UEBwwH
    VG9yb250bzEPMA0GA1UECgwGUmVkSGF0MQwwCgYDVQQLDANBQ00xRTBDBgNVBAMM
    PGdvZ3Mtc3ZjLWRlZmF1bHQuYXBwcy5yanVuZy1odWIxMy5kZXYwNi5yZWQtY2hl
    c3RlcmZpZWxkLmNvbTEfMB0GCSqGSIb3DQEJARYQcm9rZWpAcmVkaGF0LmNvbTCC
    AiIwDQYJKoZIhvcNAQEBBQADggIPADCCAgoCggIBAM3nPK4mOQzaDAo6S3ZJ0Ic3
    U9p/NLodnoTIC+cn0q8qNCAjf13zbGB3bfN9Zxl8Q5fv+wYwHrUOReCp6U/InyQy
    6OS3gj738F635inz1KdyhKtlWW2p9Ye9DUtx1IlfHkDVdXtynjHQbsFNIdRHcpQP
    upM5pwPC3BZXqvXChhlfAy2m4yu7vy0hO/oTzWIwNsoL5xt0Lw4mSyhlEip/t8lU
    xn2y8qhm7MiIUpXuwWhSYgCrEVqmTcB70Pc2YRZdSFolMN9Et70MjQN0TXjoktH8
    PyASJIKIRd+48yROIbUn8rj4aYYBsJuoSCjJNwujZPbqseqUr42+v+Qp2bBj1Sjw
    +SEZfHTvSv8AqX0T6eo6njr578+DgYlwsS1A1zcAdzp8qmDGqvJDzwcnQVFmvaoM
    gGHCdJihfy3vDhxuZRDse0V4Pz6tl6iklM+tHrJL/bdL0NdfJXNCqn2nKrM51fpw
    diNXs4Zn3QSStC2x2hKnK+Q1rwCSEg/lBawgxGUslTboFH77a+Kwu4Oug9ibtm5z
    ISs/JY4Kiy4C2XJOltOR2XZYkdKaX4x3ctbrGaD8Bj+QHiSAxaaSXIX+VbzkHF2N
    aD5ijFUopjQEKFrYh3O93DB/URIQ+wHVa6+Kvu3uqE0cg6pQsLpbFVQ/I8xHvt9L
    kYy6z6V/nj9ZYKQbq/kPAgMBAAEwDQYJKoZIhvcNAQELBQADggIBAKZuc+lewYAv
    jaaSeRDRoToTb/yN0Xsi69UfK0aBdvhCa7/0rPHcv8hmUBH3YgkZ+CSA5ygajtL4
    g2E8CwIO9ZjZ6l+pHCuqmNYoX1wdjaaDXlpwk8hGTSgy1LsOoYrC5ZysCi9Jilu9
    PQVGs/vehQRqLV9uZBigG6oZqdUqEimaLHrOcEAHB5RVcnFurz0qNbT+UySjsD63
    9yJdCeQbeKAR9SC4hG13EbM/RZh0lgFupkmGts7QYULzT+oA0cCJpPLQl6m6qGyE
    kh9aBB7FLykK1TeXVuANlNU4EMyJ/e+uhNkS9ubNJ3vuRuo+ECHsha058yi16JC9
    NkZqP+df4Hp85sd+xhrgYieq7QGX2KOXAjqAWo9htoBhOyW3mm783A7WcOiBMQv0
    2UGZxMsRjlP6UqB08LsV5ZBAefElR344sokJR1de/Sx2J9J/am7yOoqbtKpQotIA
    XSUkATuuQw4ctyZLDkUpzrDzgd2Bt+aawF6sD2YqycaGFwv2YD9t1YlD6F4Wh8Mc
    20Qu5EGrkQTCWZ9pOHNSa7YQdmJzwbxJC4hqBpBRAJFI2fAIqFtyum6/8ZN9nZ9K
    FSEKdlu+xeb6Y6xYt0mJJWF6mCRi4i7IL74EU/VNXwFmfP6IadliUOST3w5t92cB
    M26t73UCExXMXTCQvnp0ki84PeR1kRk4
    -----END CERTIFICATE-----
```

2. Configure the channel with this config map like this.

```
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: my-channel
  namespace: channel-ns
spec:
  configMapRef:
    name: git-ca
  pathname: <Git URL>
  type: Git
```

## Client certificate for mTLS connection

If a Git server requires client certificate verification for mTLS connection, use this channel configuration to set client certificate for connecting to the Git server.

1. Create a secret to contain client private key in `clientKey` field and public certificate in `clientCert` field under `data` in the same namespace as the channel. It is easier to create this secret using a kubectl command, for example, `kubectl create secret generic git-client-cert --from-file=clientKey=./client.key --from-file=clientCert=./client.crt`. 

```
apiVersion: v1
kind: Secret
metadata:
  name: git-client-cert
  namespace: channel-ns
data:
  clientCert: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUZhRENDQTFDZ0F3SUJBZ0lKQUlPancyS0hUaG9GTUEwR0NTcUdTSWIzRFFFQkN3VUFNR2d4Q3pBSkJnTlYKQkFZVEFsVlRNUXN3Q1FZRFZRUUlEQUpPUXpFUU1BNEdBMVVFQnd3SFVtRnNaV2xuYURFUE1BMEdBMVVFQ2d3RwpVbVZrU0dGME1RNHdEQVlEVlFRTERBVlNTRUZEVFRFWk1CY0dBMVVFQXd3UVoyOW5jeTF6ZG1NdFpHVm1ZWFZzCmREQWVGdzB5TVRFeE1qWXlNREkwTURCYUZ3MHlNekEwTVRBeU1ESTBNREJhTUdNeEN6QUpCZ05WQkFZVEFrTkIKTVFzd0NRWURWUVFJREFKUFRqRVFNQTRHQTFVRUJ3d0hWRzl5YjI1MGJ6RVBNQTBHQTFVRUNnd0dVbVZrU0dGMApNUTR3REFZRFZRUUxEQVZTU0VGRFRURVVNQklHQTFVRUF3d0xUV0ZqUW05dmF5MVFjbTh3Z2dJaU1BMEdDU3FHClNJYjNEUUVCQVFVQUE0SUNEd0F3Z2dJS0FvSUNBUUM5NVdGZzhSY05zZ0JnNkJ2MjAvRm5NNWgwcnRtQy9kNmoKeTVHWnVBekw0Vk40S2pwOU5nNWxvd0dyRXpyMDlsMXc2eEVyb2trVmNyTVFMdVBybEE4cStscHhrcTcvU1hjbwoyT28wWmNDNkdDSy9aRXExdUdQUmFTOVkvQ25oRE8zdlhiZjc3N01OL3RndDMyUzUyd1kwNHpUMEZBOFBrWDNLCjZhbE5ub21sNFJ4d0h6MWgzZHV4L21NaWt1T1UrSmxXMGJsZzVEM2ZLeWxEQXd1QWZicUxsWDJ1T3MrMkswWUMKVXRyYWdsdjZYOEtRK0l0aUhxMWoxQU9TZEQ0TEFPYjhOV1ZnM0xxaTZiandPOXYvYmJ1YUxaMjF2UDB1VUk3UgpSWEF1SHI5bkcrMkhRQ1gzelRUOTNjMmMzeWtzb3dmUlpDM282a1RSUlJRLzhwS0kvZnhLTWpadmdNVVgwUlhiCkpCTU5ocnpReEVoS3c2YU5IaHFzaThCZFJmT3NnUFBXRFF4WEtFb1ZmSHQ3TWV0S3FvVVY1M2FxWVNrbmF2RDcKQ0tRNVYvT1J3aGhuam9uMzdpV2dQcVlWVVIySmxjU25Ec3N2U0ZtdmRuUkdrQWVYb2ErVk5DSVp4TVFYc2FMQgpFV29hci9lRVdjRlE1bVFpZVVPWXlKQ25WdkQrclR6bWxxM0d1M1R5My9xdlVKbHY0a0tvTXNNQzlQZ3NwbE04Ck45UFdjdytoVDlucjBwbEtzejkzTUgzRk16VHQzcGVEU0JWTTU5Y2hyM2VEOWRnRCtlZlVuK2lyUFY1TTNqeEgKTTJkazdmQWlsbGZnV2wvNmN4RG1qdWdlNU5ZUGpLRno5TE5Ja1BvU1hRQVlid1ZqcHJweUlvZmFON3JSNjVPVApFNnBOZUVoQit3SURBUUFCb3hvd0dEQVdCZ05WSFJFRUR6QU5nZ3ROWVdOQ2IyOXJMVkJ5YnpBTkJna3Foa2lHCjl3MEJBUXNGQUFPQ0FnRUFlS3Z5STVpSWVZbjNQdFA1L1A3RkZWRjVTMTBpSTVuNTlFZm1ZNEVzVUlGMUY1RWwKc1JSSWExTitzYXhTaGFvRkFHdnVvNllsbGVUa3p0ck5odmd6MVRzTHdLVFMrUjFYbmVQSnN4cXdDQXY1K0lWZAo5YU1mMko2UGxNalJPT3ZubG9ZRW5wK0g5Qmhpb0YxTXBvSG8wTG1INkQ4NEI0QVFNOUlGRmF4VTY3RFpVR3doCkFaUFRrQXBubHpkQ0RxYUpqdERURCtFeVc4Nk5jTTFYeXNUcTd4dE9YUDVWU0dqYk9wZUxmUlluc2k1RW1sZmMKMmgzeTFQTkR6UzMzUHJDcXJNZm1TRzhDcUVXTytDVmhFSTBPd1NiVlNvU1NEM3hmWmlZdlJtTTVtWGsyZzhCcApKa3VBYUcvSzZaVFJySGxLZW0wTGhZWktBV1k0VGpVczZ1cjR5V1BDbm9MMXNtMlRsekdBVERrRExSdU5nbUY5CnBvTEtsY3VPR2JpNkMvY0Q0TnRITWtCcXF3Ky9OZWUvdFU0enRWMWMyWERqUFBPTWRHOENIQlczNEJEc0loVUMKM093OEw5TVBxQXY2dFo2M0FnaU55UnVPK2NpbGxmUENndmNqc1oyOTB0ZWpoTTJ3SVdQUkd2ZWhSVi9FSHZ0Ywo5SHhTUFBuYWdMdU5xSkdLSUVhNnplc2hIN0hzVUFObXQ0SGZRRmhIWk5BYVVtVkd1V2RDVXREQzdMWHVvN0V0Cno1dEE3a3JWZnpxSVFkdkVhdnpEejdOU0RYT3VtYVMxNzlOWVNFcFRCWmR2bit0QVFxMmVVYVR5ZmUxck9LUkMKUmpTV2MxNUhKRDkxc0ZZSlhwZk9QOStMc2tvRTYxSUw2aFJYUTRxK0g5eWRneGhvYjEzelErNUhoM1U9Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K
  clientKey: LS0tLS1CRUdJTiBSU0EgUFJJVkFURSBLRVktLS0tLQpNSUlKS2dJQkFBS0NBZ0VBdmVWaFlQRVhEYklBWU9nYjl0UHhaek9ZZEs3Wmd2M2VvOHVSbWJnTXkrRlRlQ282CmZUWU9aYU1CcXhNNjlQWmRjT3NSSzZKSkZYS3pFQzdqNjVRUEt2cGFjWkt1LzBsM0tOanFOR1hBdWhnaXYyUksKdGJoajBXa3ZXUHdwNFF6dDcxMjMrKyt6RGY3WUxkOWt1ZHNHTk9NMDlCUVBENUY5eXVtcFRaNkpwZUVjY0I4OQpZZDNic2Y1aklwTGpsUGlaVnRHNVlPUTkzeXNwUXdNTGdIMjZpNVY5cmpyUHRpdEdBbExhMm9KYitsL0NrUGlMClloNnRZOVFEa25RK0N3RG0vRFZsWU55Nm91bTQ4RHZiLzIyN21pMmR0Yno5TGxDTzBVVndMaDYvWnh2dGgwQWwKOTgwMC9kM05uTjhwTEtNSDBXUXQ2T3BFMFVVVVAvS1NpUDM4U2pJMmI0REZGOUVWMnlRVERZYTgwTVJJU3NPbQpqUjRhckl2QVhVWHpySUR6MWcwTVZ5aEtGWHg3ZXpIclNxcUZGZWQycW1FcEoycncrd2lrT1ZmemtjSVlaNDZKCjkrNGxvRDZtRlZFZGlaWEVwdzdMTDBoWnIzWjBScEFIbDZHdmxUUWlHY1RFRjdHaXdSRnFHcS8zaEZuQlVPWmsKSW5sRG1NaVFwMWJ3L3EwODVwYXR4cnQwOHQvNnIxQ1piK0pDcURMREF2VDRMS1pUUERmVDFuTVBvVS9aNjlLWgpTck0vZHpCOXhUTTA3ZDZYZzBnVlRPZlhJYTkzZy9YWUEvbm4xSi9vcXoxZVRONDhSek5uWk8zd0lwWlg0RnBmCituTVE1bzdvSHVUV0Q0eWhjL1N6U0pENkVsMEFHRzhGWTZhNmNpS0gyamU2MGV1VGt4T3FUWGhJUWZzQ0F3RUEKQVFLQ0FnQVFEaVhHa1h1MmY3ZENlajFyQ0E1Z0FGL0NkY29VSml5OXdvZGo3TWpBTUNQamhBWi84YU1UK3MwNApUcDdzZVN2N1VSU1prTllIQnpTV2lMQmlpWmtpdEJvVGdpaGpreTJNK1pJTHJoSzJhVFgrNHBiaWVGMlFKZ21ICng0SXU4ZTNvRUE5dk1Kdjk4ZThMS2RrVlVheGJxbDRleU5kaEpKYnpJZ0JiZkt6OHBtVEdJZzRKaVQxNjlod2gKbnpPNHM3QWJSQTRkWUE3UElKSVRoVjFpbHI2LzhIcTQwV1lnYTlZMmVoVGc1cWFxSzVDQ3UxRGxrZHh0RlU3YwpBaElBMGltMVp6cFVEeldFY3FoZFAySmhYVURBVDEwSE1aeGlYeE5FSkUvUXhtNitaQUVZTDFkR2RVWW10S3NlCnVVdDg1NmZRajF6R1ZaQk45VDQ2RGVOUHk5R1p1dDhzaXRzVWtVWUtlZ3dISmFHSXlNQnpLVkRCWW9hM0F0alMKZXJUOWVTTE5uYnlYUExuZndIU1Raa3UvUkRYN0lhTmtaL3g0S3o0eTZlSHlJNnFxcnkwek1oZDR6K0IwMmJGdgpheUdiVldPaTZiRkZRUVkwclBFVEUrOGw1YzUyK2R5ZHVNY25wT2VRYWJLSDRsZDhoQ0lOSU5OZSttR3dXVjZjCkZDcGZOUG5nOUR3M0tmVlJjckFXMkVuL1lQZHJkVzVoYzhsaFM3QWZhRkJKNUVCQVlWdXNGS2NNeS9vOXVmTVMKaC9iZWt0eDNzb0kxWGtkRCtlL1lWZHRjSmp5eUlpY1B6UUk3b2tlRWxMT0VXWEVFemJ2cDREUzR5bFdYU0Y1LwpVZmFwakExQkNLaU5UUmVtRktpZm5SQ3U4clM0ZWVOaFBOKzRKT2lyZG50SEJESW0wUUtDQVFFQTg1SUpSMnQwCktJcU1MTUVhZHNYRmRtQklCUFdEMXpTVkRabG1PYXBGcHFQcmtFZ0RQOU9EVWJVSnBLZ25SdERncmp4dWQwMWsKdVJiNHZ6VE5NZmJYS0JXdmRUOWUwNHJVN0YrcVlPQThBdkZ3K3RSZDdRcEdYYm1sTFRHbnBKRUlJejZnNjBvbQpUNUNVSEFtRG9JaTRXamQ0Zkc0MGZZV01WZnJlNFRXaHFOTDIyNmRLeDZyalh1czhzSFRpcG0wNm9uVytrdjRaCllVWkZDV2xGN2Z6QzJueDZNREFLNForYVRuTGhFS3g5cDhXdzhGVFFsNWl5elp2WU1WcWp0am5xTmk4Rko3d0YKTm5QVEprTWpic3RYYVRXN1VWVzF0R2NoRXVMRlZWMTdzcmk4N1BVWldSYUdOdFFGYVhkMW5TK292UnUrMjlSWgpuWEdDQUpoa2lZbEVvd0tDQVFFQXg1WW1keGVaTTFtblpBM1hGSTI3bUNQNzAxam1BeldyM1ZXRlJzMUFVTFp5Cmtob3JwdjI3cjhYWmdwVkIrZndJNDF5YkJpT2tabTg3S0JXbkhtdFJhTGd0L1lvNHFvNENWd1ZFNDRoelhiM08KWXp5VmQyem1rbU5QUFg2cjQvZ2ZMMDdGSjhyMXhQcFNPdzBUOHBZVEJ0dXptT3lQVkMvRENBd2hJVTAvd1lLLwo2RCtSKzIrMy84UFdsazhxRm9NM2VCbVMwd25mU2xndEs1RGJFRG1DbFNZZTl6cnZZYTExcVRsOFhac0lVZ2UyClpHcFRpcWl2QzlFZjRuRXR2VEg4d3dYV0lWNE90a2g3bmtFczVKV1BJRmxIMGJXNGhhSXFlMWFLR0xCZFpMVEgKOGFHNStmdFZQNEVHZy80eW9Mb3VPWldMRzR0UWYwRGRtWHNXTUJFS3lRS0NBUUVBenVjejJLWnZ3dXlHVEpJdwpuOG82aGVjZDVwQ0VVVEJLbVlYQWI2V3ppdTNkOGU4cVMzS3FMNk5Bc00xaXArWlJwZENSNmVnbGNwRVA5cVNzCjFnK2dEMTMwY1AzRzJKNHJaeXVRdU1CaVdnR3MvbldkMy9rRmN5SjdMSnZzazMvYjBNeW50NWVDV2I0d0FtMFcKa2l6eHBUbFBzU3VRalR2L3pSM1JiSys1Um9jR3llb2ZQT051UjdnK0Vvc0g2V1lLRSswL1dQQmVzT0lYVFBwUApPKzJsdkNGWFRrM1JRSDNxZjA5WTNtZ1lTWWRHV2JGQ2pEUmJWd3RXcC80ZDZYTGpBT29LVUFtSXlBdTF4dC9yClhuZC9KbEZOY0xjRkpsMGduRUZrYmJKK25JQVBoSjVqek9pMFZDamcvQWxOTUp4R2szWFpPRFgzZGlYd3ovNmsKRUswNVR3S0NBUUVBdkQxR2JCNUt0TjdDL0ovOUVmcVhTdWZ0VkNsTlR3dW8yK2tUK1hJT1RkRzEvMHhGall0VQpJSFVYTDR6OG0vejBtUk8zZHJQWkUyK25PS2dyWllwTVR0c2ZMVGpYTnkwVzBlSTdWZXBVL0wzTzBWUExtTWFTCk5ORUxaSEY3UjFpMmVOVHZLQmZKY3cyd3p0Y0gwRTF4Qm8vaU5NSXdjSE5YbTloc3lzN2o2ZXdhZWI2elBaVGgKMG8xdmIzYWN1SHpyclE5WnBrZHlYTmtMU1VLbkNnZGp2SzRtbUlEeVU0clpKVkQ5bGY5cGVveWhudFdWazNxRApGajhYdG1lT2Z4bU5UcGJMbmM3clMwclRwSy9OMm8yZ1h4ODFtSG11M2dMVDFGNExnenViNmlhRFNjZUI4MTd6CnlTQTA5L3FnMzJ5WnVsL3J3bi9EM1RGZGo2dENndlVmMlFLQ0FRRUEzUndWaGhZTDAwR1lRN1NlU0FRNlAzTFEKMTlneENBdVZUSFRKUU9LWSsxRENkRFlNSm00Y0V6dXN2YitCS0d6aFVMMm5VbzYrbm5CUktyd2UwUVBjUkFkbwpnY0paczRzK2krVktjcnFxOFJScmppR1k1UHpxQXQzTnRiQ2t4TDdBMFUydFB2blc0ZFZIS1ZEUGdVdUJScFBMCnVEUWVIQ0ZETUYwRi9rWWQ1NkhSaEJra3ZUTzR0VHJGTHVUM1RaTHNGcktTaVRxUTZKWFA4cHJBcHVuU1h0dFoKc3FwNkRnTGczSHBCWE9FNmpJU3QwaVFLenVBbENwSlJ1WDYrcE41cVc1dTdrM3VUcWUxN09xdjFoRk45S1lwaApzdURzSjRTaXQ2V3RZUlRyQ1FTK0hJaFZ5VnlZRkQyQTBYOG1kRXFRRmYrbitMc3VGRmc4cThsclY3cXg0UT09Ci0tLS0tRU5EIFJTQSBQUklWQVRFIEtFWS0tLS0tCg==
type: Opaque
```

2. Configure the channel with this secret like this.

```
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: my-channel
  namespace: channel-ns
spec:
  secretRef:
    name: git-client-cert
  pathname: <Git URL>
  type: Git
```

## Git SSH connection

1. Create a secret to contain your private SSH key in `sshKey` field under `data`. If the key is passphrase protected, specify the password in `passphrase` field. This secret must be in the same namespace as the channel CR. It is easier to create this secret using a kubectl command like `kubectl create secret generic git-ssh-key --from-file=sshKey=./.ssh/id_rsa` and then add base64 encoded `passphrase`.

```
apiVersion: v1
kind: Secret
metadata:
  name: git-ssh-key
  namespace: channel-ns
data:
  sshKey: LS0tLS1CRUdJTiBPUEVOU1NIIFBSSVZBVEUgS0VZLS0tLS0KYjNCbGJuTnphQzFyWlhrdGRqRUFBQUFBQ21GbGN6STFOaTFqZEhJQUFBQUdZbU55ZVhCMEFBQUFHQUFBQUJDK3YySHhWSIwCm8zejh1endzV3NWODMvSFVkOEtGeVBmWk5OeE5TQUgcFA3Yk1yR2tlRFFPd3J6MGIKOUlRM0tKVXQzWEE0Zmd6NVlrVFVhcTJsZWxxVk1HcXI2WHF2UVJ5Mkc0NkRlRVlYUGpabVZMcGVuaGtRYU5HYmpaMmZOdQpWUGpiOVhZRmd4bTNnYUpJU3BNeTFLWjQ5MzJvOFByaDZEdzRYVUF1a28wZGdBaDdndVpPaE53b0pVYnNmYlZRc0xMS1RrCnQwblZ1anRvd2NEVGx4TlpIUjcwbGVUSHdGQTYwekM0elpMNkRPc3RMYjV2LzZhMjFHRlMwVmVXQ3YvMlpMOE1sbjVUZWwKSytoUWtxRnJBL3BUc1ozVXNjSG1GUi9PV25FPQotLS0tLUVORCBPUEVOU1NIIFBSSVZBVEUgS0VZLS0tLS0K
  passphrase: cGFzc3cwcmQK
type: Opaque
```

2. Configure the channel with this secret like this.

```
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: my-channel
  namespace: channel-ns
spec:
  secretRef:
    name: git-ssh-key
  pathname: <Git SSH URL>
  type: Git
```

3. The subscription controller does `ssh-keyscan` with the provided Git hostname to build the known_hosts list to prevent MITM attack in SSH connection. If you want to skip this and make insecure connection, use `insecureSkipVerify: true` in the channel configuration.

```
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: my-channel
  namespace: channel-ns
spec:
  secretRef:
    name: git-ssh-key
  pathname: <Git SSH URL>
  type: Git
  insecureSkipVerify: true
```

## GitHub SSH connection
GitHub offers two ways to connect via SSH. The default connection via port `22` or by using SSH over the HTTPS port `443`. Use the following channel configurations to configure your connection type.

1. Create a secret to contain your private SSH key in `sshKey` field under `data`. If the key is passphrase protected, specify the password in `passphrase` field. This secret must be in the same namespace as the channel CR. It is easier to create this secret using a kubectl command like `kubectl create secret generic git-ssh-key --from-file=sshKey=./.ssh/id_rsa` and then add base64 encoded `passphrase`.

```
apiVersion: v1
kind: Secret
metadata:
  name: git-ssh-key
  namespace: channel-ns
data:
  sshKey: LS0tLS1CRUdJTiBPUEVOU1NIIFBSSVZBVEUgS0VZLS0tLS0KYjNCbGJuTnphQzFyWlhrdGRqRUFBQUFBQ21GbGN6STFOaTFqZEhJQUFBQUdZbU55ZVhCMEFBQUFHQUFBQUJDK3YySHhWSIwCm8zejh1endzV3NWODMvSFVkOEtGeVBmWk5OeE5TQUgcFA3Yk1yR2tlRFFPd3J6MGIKOUlRM0tKVXQzWEE0Zmd6NVlrVFVhcTJsZWxxVk1HcXI2WHF2UVJ5Mkc0NkRlRVlYUGpabVZMcGVuaGtRYU5HYmpaMmZOdQpWUGpiOVhZRmd4bTNnYUpJU3BNeTFLWjQ5MzJvOFByaDZEdzRYVUF1a28wZGdBaDdndVpPaE53b0pVYnNmYlZRc0xMS1RrCnQwblZ1anRvd2NEVGx4TlpIUjcwbGVUSHdGQTYwekM0elpMNkRPc3RMYjV2LzZhMjFHRlMwVmVXQ3YvMlpMOE1sbjVUZWwKSytoUWtxRnJBL3BUc1ozVXNjSG1GUi9PV25FPQotLS0tLUVORCBPUEVOU1NIIFBSSVZBVEUgS0VZLS0tLS0K
  passphrase: cGFzc3cwcmQK
type: Opaque
```

2. GitHub offers two ways to connect via SSH. The first way is connecting through the default port `22` or by using SSH over the HTTPS port `443`. 

Using the default port `22` configure the channel with the secret like this.

```
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: my-channel
  namespace: channel-ns
spec:
  secretRef:
    name: git-ssh-key
  pathname: ssh://git@github.com/<GitHub repository>
  type: Git
```

Using SSH over HTTPS port `443` configure the channel with the secret like this. Note the hostname for port `443` is `ssh.github.com`, not `github.com`.

```
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: my-channel
  namespace: channel-ns
spec:
  secretRef:
    name: git-ssh-key
  pathname: ssh://git@ssh.github.com:443/<GitHub repository>
  type: Git
```

3. The subscription controller does `ssh-keyscan` with the provided Git hostname to build the known_hosts list to prevent MITM attack in SSH connection. If you want to skip this and make insecure connection, use `insecureSkipVerify: true` in the channel configuration.

```
apiVersion: apps.open-cluster-management.io/v1
kind: Channel
metadata:
  name: my-channel
  namespace: channel-ns
spec:
  secretRef:
    name: git-ssh-key
  pathname: <GitHub SSH URL>
  type: Git
  insecureSkipVerify: true
```

## Updating channel secret and config map

If Git channel connection configuration, such as CA certificates, credentials, or SSH key, requires an update, create new secret and config map in the same namespace and update the channel to reference the new secret and configmap.