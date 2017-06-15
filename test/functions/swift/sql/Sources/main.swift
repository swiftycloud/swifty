import SwiftKuery
import SwiftKueryMySQL
import Foundation

let envs = ProcessInfo.processInfo.environment

var mwName: String
mwName = String(CommandLine.arguments[2]).uppercased()

let addr_port = envs["MWARE_" + mwName + "_ADDR"]
let addr_port_a = addr_port!.components(separatedBy: ":")
let user = envs["MWARE_" + mwName + "_USER"]
let pass = envs["MWARE_" + mwName + "_PASS"]
let dbnm = envs["MWARE_" + mwName + "_DBNAME"]

let conn = MySQLConnection(host: addr_port_a[0],
			user: user, password: pass, database: dbnm,
			port: Int(addr_port_a[1]))

class DBEnt: Table {
	let tableName = "bar"
	let name = Column("name", Varchar.self, length:32)
	let age = Column("age", Int32.self)
}

let ents = DBEnt()

conn.connect() { error in
	if error != nil {
		print(error as Any)
		return
	}

	if CommandLine.arguments[1] == "create" {
		ents.create(connection: conn) { cres in
			if let error = cres.asError {
				print(error as Any)
				return
			}
		}
	} else if CommandLine.arguments[1] == "insert" {
		let val = CommandLine.arguments[3]
		let iq = Insert(into: ents, columns: [ents.name, ents.age], values: [val, 12])
		conn.execute(query: iq) { ires in
			if let error = ires.asError {
				print(error as Any)
				return
			}
		}
	} else if CommandLine.arguments[1] == "select" {
		let query = Select(from: ents)
		conn.execute(query: query) { qres in
			if let rs = qres.asResultSet {
				for row in rs.rows {
					let val = row[0] as! String
					print("swift:sql:" + val)
				}
			}
		}
	}
}
