package polymarket

const (
	// ParentCollectionID is the default parent collection ID for conditional tokens
	ParentCollectionID = "0x0000000000000000000000000000000000000000000000000000000000000000"

	// PolygonChainID is the Polygon mainnet chain ID
	PolygonChainID = 137
)

// ConditionalTokensABI is the ABI for the redeemPositions function
const ConditionalTokensABI = `[
	{"constant":false,"inputs":[
		{"name":"collateralToken","type":"address"},
		{"name":"parentCollectionId","type":"bytes32"},
		{"name":"conditionId","type":"bytes32"},
		{"name":"indexSets","type":"uint256[]"}
	],"name":"redeemPositions","outputs":[],"payable":false,"stateMutability":"nonpayable","type":"function"}
]`

// SafeABI is the ABI for Gnosis Safe contract
const SafeABI = `[
  {"inputs":[],"name":"nonce","outputs":[{"internalType":"uint256","name":"","type":"uint256"}],"stateMutability":"view","type":"function"},
  {"inputs":[
    {"internalType":"address","name":"to","type":"address"},
    {"internalType":"uint256","name":"value","type":"uint256"},
    {"internalType":"bytes","name":"data","type":"bytes"},
    {"internalType":"uint8","name":"operation","type":"uint8"},
    {"internalType":"uint256","name":"safeTxGas","type":"uint256"},
    {"internalType":"uint256","name":"baseGas","type":"uint256"},
    {"internalType":"uint256","name":"gasPrice","type":"uint256"},
    {"internalType":"address","name":"gasToken","type":"address"},
    {"internalType":"address","name":"refundReceiver","type":"address"},
    {"internalType":"bytes","name":"signatures","type":"bytes"}
  ],"name":"execTransaction","outputs":[{"internalType":"bool","name":"success","type":"bool"}],"stateMutability":"payable","type":"function"}
]`

// PayoutRedemptionEventABI is the ABI for the PayoutRedemption event
const PayoutRedemptionEventABI = `[
  {
    "anonymous": false,
    "inputs": [
      {"indexed": true,  "name": "redeemer",        "type": "address"},
      {"indexed": true,  "name": "collateralToken", "type": "address"},
      {"indexed": true,  "name": "parentCollectionId","type":"bytes32"},
      {"indexed": false, "name": "conditionId",     "type": "bytes32"},
      {"indexed": false, "name": "indexSets",       "type": "uint256[]"},
      {"indexed": false, "name": "payout",          "type": "uint256"}
    ],
    "name": "PayoutRedemption",
    "type": "event"
  }
]`
