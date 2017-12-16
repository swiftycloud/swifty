Setup Swifty
************

Overview
========

Swifty is a set of distributed components:

* ``swy-gate`` daemon which serves incoming HTTP requests and may be considered
  as a gate to all underlied serivces
* ``swy-wdog`` daemon which runs inside each container serving as a watchdog
* ``swy-s3`` daemon which provide S3 compatible layer
* Openstack keystone to provide client authentication
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
addresses may be different of course) and can reach each other

.. code-block:: none

        10.94.96.220            crvm3
        10.94.96.219            crvm4
        10.94.96.221            crvm5

Node **crvm3** will lead the orchestra and run ``swy-gate`` and ``swy-s3``
daemons. While **crvm4** and **crvm5** are for middleware and containers.

+-------+-------------------------------+
| Host  | Component                     |
+=======+===============================+
| crvm3 | ``swy-gate``                  |
|       +-------------------------------+
|       | ``swy-s3``                    |
|       +-------------------------------+
|       | Kubernetes master             |
|       +-------------------------------+
|       | Keynote + MariaDB             |
|       +-------------------------------+
|       | MongoDB                       |
|       +-------------------------------+
|       | NFS                           |
|       +-------------------------------+
|       | Ceph                          |
+-------+-------------------------------+
| crvm4 | ``swy-wdog``                  |
|       +-------------------------------+
|       | Middleware                    |
|       +-------------------------------+
|       | Kubernetes slave              |
|       +-------------------------------+
|       | Docker                        |
|       +-------------------------------+
|       | NFS                           |
|       +-------------------------------+
|       | Ceph                          |
+-------+-------------------------------+
| crvm5 | ``swy-wdog``                  |
|       +-------------------------------+
|       | Kubernetes slave              |
|       +-------------------------------+
|       | Docker                        |
|       +-------------------------------+
|       | NFS                           |
|       +-------------------------------+
|       | Ceph                          |
+-------+-------------------------------+

Preparing the nodes
===================

The following programs are to be installed on all nodes.

.. code-block:: text

        dnf -y install rsync wget git ipvsadm libselinux-python
        dnf -y install nfs-utils ntp ntpdate

Disable firewall and enable NFS service

.. code-block:: text

        systemctl stop firewalld
        systemctl disable firewalld
        systemctl start nfs
        systemctl enable nfs

Disable SELinux via ``/etc/sysconfig/selinux``

.. code-block:: text

        SELINUX=disabled

Finally reboot the nodes.

Setup Keystone
==============

Follow the `instructions <https://docs.openstack.org/keystone/latest/install/index.html>`_
to install `Pike <https://www.openstack.org/software/releases/ocata/components/keystone>`_
keystone.

Install MariaDB for keystone use

.. code-block:: text

        dnf -y install mariadb mariadb-server-utils
        systemctl start mariadb
        systemctl enable mariadb

Configure the database

.. code-block:: text

        mysqladmin -u root password aiNe1sah9ichu1re
        mysql -u root -p
        CREATE DATABASE keystone;
        GRANT ALL PRIVILEGES ON keystone.* TO 'keystone'@'localhost' \
        IDENTIFIED BY 'Ja7hey6keiThoyoi';
        GRANT ALL PRIVILEGES ON keystone.* TO 'keystone'@'%' \
        IDENTIFIED BY 'Ja7hey6keiThoyoi';

These passwords may be different of course: ``aiNe1sah9ichu1re`` comes for
global administration and ``Ja7hey6keiThoyoi`` is solely for access from
keystone.

Install required packages (simply search for ``rdo-release-pike.rpm``).

.. code-block:: text

        dnf install https://goo.gl/qsXjTf
        dnf -y install openstack-keystone httpd mod_wsgi python-openstackclient

Configure ``/etc/keystone/keystone.conf``

.. code-block:: text

        [database]
                ...
                connection = mysql+pymysql://keystone:Ja7hey6keiThoyoi@controller/keystone

        [token]
                ...
                provider = fernet

Prepare the keystone

.. code-block:: text

        /bin/sh -c "keystone-manage db_sync" keystone
        keystone-manage fernet_setup --keystone-user \
        	keystone --keystone-group keystone
        keystone-manage credential_setup --keystone-user \
        	keystone --keystone-group keystone

        keystone-manage bootstrap --bootstrap-password Cae6ThiekuShiece \
          --bootstrap-admin-url http://controller:35357/v3/ \
          --bootstrap-internal-url http://controller:5000/v3/ \
          --bootstrap-public-url http://controller:5000/v3/ \
          --bootstrap-region-id RegionOne

In ``/etc/httpd/conf/httpd.conf`` setup server name

.. code-block:: text

        ServerName controller

and add this alias to ``/etc/hosts``

.. code-block:: text

        127.0.0.1               controller

Link the wsgi config and start web server

.. code-block:: text

        ln -s /usr/share/keystone/wsgi-keystone.conf /etc/httpd/conf.d/
        systemctl enable httpd.service
        systemctl start httpd.service

Put the folling variables into ``~/.bashrc``

.. code-block:: shell

        export OS_USERNAME=admin
        export OS_PASSWORD=Cae6ThiekuShiece
        export OS_PROJECT_NAME=admin
        export OS_USER_DOMAIN_NAME=Default
        export OS_PROJECT_DOMAIN_NAME=Default
        export OS_AUTH_URL=http://controller:5000/v3
        export OS_IDENTITY_API_VERSION=3

Now setup a role, a user and a project. There are three roles
used by swifty. The swifty.admin is allowed to manage users,
swifty.owner is allowed to work with gate on his project and
manage himself, and swifty.ui is allowed to create new users.

All users and projects should (but not must) live in a separate
domain, as swy-admd lists all users in it.

.. code-block:: text

        openstack role create swifty.admin
        openstack role create swifty.owner
        openstack role create swifty.ui
        openstack domain create swifty
        openstack project create --domain swifty swyadmin
        openstack user create --project swyadmin --domain swifty --password 1q2w3e swyadmin
        openstack role add --user-domain swifty --user swyadmin --project-domain swifty --project swyadmin swifty.admin
        openstack role add --user-domain swifty --user swyadmin --project-domain swifty --project swyadmin swifty.owner

Now the swyadmin is registered as both, admin and owner.
