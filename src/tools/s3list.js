conn = new Mongo()
db = conn.getDB("swifty-s3")

print("Namespaces:")
db.S3AccessKeys.find().forEach(function(i) { print(i["namespace"]) })
print("Buckets:")
buckets = {}
db.S3Buckets.find().forEach(function(i) { buckets[i._id] = i.name; print(i.name) } )
print("Objects:")
db.S3Objects.find().forEach(function(i) { print(buckets[i["bucket-id"]] + "/" + i.name) })
