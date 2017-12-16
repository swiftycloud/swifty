Setup Swifty
************

Components
==========

Swifty is a set of distributed components:

* ``swy-gate`` daemon which serves incoming HTTP requests and may be considered
  as a gate to all underlied serivces
* ``swy-wdog`` daemon which runs inside each container serving as a watchdog
* ``swy-s3`` daemon which provide S3 compatible layer
* Ceph instance to provide persistent storage
* Kubernetes which manage resources requested by swifty
* Docker which spawns and deletes containers
* NFS server and client to access functions source code to run inside containers
* Mongo database used by swifty to track resources
* Middleware programs (MariaDB, RabbitMQ and etc) to serve requests from containers

Each of these components may preset on a sole hardware node or may be
completely distributed in one per node way.

To provide an example of the setup procedure we create three virtual machines
**crvm3**, **crvm4** and **crvm5** based on `Fedora 26 <https://getfedora.org/en/workstation/download/>`_
distributive.

Make sure all three machines are having same ``/etc/hosts`` contents (the
addresses may be different of course)

.. code-block:: none

        10.94.96.220            crvm3
        10.94.96.219            crvm4
        10.94.96.221            crvm5
