var isAbsolute = require('path-is-absolute')

exports.Main = function(rq) {
    return [ { message: isAbsolute(rq.args.path) }, null ]
}
