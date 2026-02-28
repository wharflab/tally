{-# LANGUAGE ForeignFunctionInterface #-}
-- | ShellCheck reactor module for WASM.
--
-- Compiled as a WASI reactor (not a command): the module exports FFI functions
-- instead of using stdin/stdout with CLI arg parsing. This eliminates format
-- dispatch, arg parsing, and stdin/stdout buffering overhead per check.
--
-- The host compiles the module once (expensive, disk-cached by wazero) and
-- instantiates a fresh instance per call. Per-call instantiation is necessary
-- because the GHC WASM RTS accumulates state in the STG evaluator that
-- corrupts after repeated varied calls to the same instance.
--
-- Per-call protocol:
--   1. Host instantiates the reactor module and calls hs_init(0, 0).
--   2. Host calls sc_alloc(n) to allocate input buffers in WASM linear memory.
--   3. Host calls sc_check(scriptPtr, scriptLen, optsPtr, optsLen, outLenPtr)
--      which returns a pointer to JSON output.
--   4. Host reads outLen bytes from the returned pointer.
--   5. Host closes the module instance (no need to sc_free; close releases all).
--
-- The JSON output matches ShellCheck's json1 format but is hand-serialized
-- (no aeson dependency).
module Main where

import Data.Functor.Identity (runIdentity)
import Data.Char (intToDigit)
import Data.List (intercalate)
import Foreign.C.String (CString, peekCStringLen, castCharToCChar)
import Foreign.C.Types (CChar(..), CInt(..))
import Foreign.Marshal.Alloc (mallocBytes, free)
import Foreign.Ptr (Ptr, plusPtr)
import Foreign.Storable (poke)

import ShellCheck.Checker (checkScript)
import ShellCheck.Interface

-- | Reactor modules must not run main.
main :: IO ()
main = pure ()

------------------------------------------------------------------------
-- Exported WASM functions
------------------------------------------------------------------------

-- | Allocate n bytes in WASM linear memory. Host writes input data here.
foreign export ccall sc_alloc :: CInt -> IO (Ptr CChar)
sc_alloc :: CInt -> IO (Ptr CChar)
sc_alloc n = mallocBytes (fromIntegral n)

-- | Free a pointer previously returned by sc_alloc or sc_check.
foreign export ccall sc_free :: Ptr CChar -> IO ()
sc_free :: Ptr CChar -> IO ()
sc_free = free

-- | Check a shell script and return JSON1-compatible output.
--
-- Parameters:
--   scriptPtr, scriptLen — the shell script text (UTF-8)
--   optsPtr, optsLen     — options as a simple line-based protocol:
--                          "dialect <name>\n"      (sh|bash|dash|ksh|busybox)
--                          "exclude <code>\n"      (integer, repeatable)
--                          "include <code>\n"      (integer, repeatable)
--                          "severity <level>\n"    (error|warning|info|style)
--                          "enable <name>\n"       (optional check name, repeatable)
--                          "extended-analysis\n"   (enable DFA)
--                          "norc\n"                (ignore .shellcheckrc)
--   outLenPtr            — host-provided pointer; sc_check writes the result length here
--
-- Returns: pointer to a JSON byte buffer (caller must sc_free it).
foreign export ccall sc_check :: Ptr CChar -> CInt -> Ptr CChar -> CInt -> Ptr CInt -> IO (Ptr CChar)
sc_check :: Ptr CChar -> CInt -> Ptr CChar -> CInt -> Ptr CInt -> IO (Ptr CChar)
sc_check scriptPtr scriptLen optsPtr optsLen outLenPtr = do
    script <- peekCStringLen (scriptPtr, fromIntegral scriptLen)
    optsStr <- peekCStringLen (optsPtr, fromIntegral optsLen)

    let opts = parseOpts (lines optsStr)
        spec = buildSpec script opts

    let result = runIdentity (checkScript newSystemInterface spec)
        jsonStr = encodeResult result

    -- Copy JSON into a fresh malloced buffer so the host can read it.
    let jsonLen = length jsonStr
    outBuf <- mallocBytes jsonLen
    pokeCString outBuf jsonStr jsonLen
    poke outLenPtr (fromIntegral jsonLen)
    return outBuf

-- | Write a Haskell String into a pre-allocated buffer byte by byte.
pokeCString :: Ptr CChar -> String -> Int -> IO ()
pokeCString ptr str len = go ptr str 0
  where
    go _ _ i | i >= len = pure ()
    go _ [] _           = pure ()
    go p (c:cs) i       = do
        poke p (castCharToCChar c)
        go (p `plusPtr` 1) cs (i + 1)

------------------------------------------------------------------------
-- Options parsing
------------------------------------------------------------------------

data Opts = Opts
    { optDialect          :: Maybe Shell
    , optExclude          :: [Integer]
    , optInclude          :: Maybe [Integer]
    , optSeverity         :: Severity
    , optEnable           :: [String]
    , optExtendedAnalysis :: Maybe Bool
    , optIgnoreRC         :: Bool
    }

defaultOpts :: Opts
defaultOpts = Opts
    { optDialect          = Nothing
    , optExclude          = []
    , optInclude          = Nothing
    , optSeverity         = StyleC
    , optEnable           = []
    , optExtendedAnalysis = Nothing
    , optIgnoreRC         = True
    }

parseOpts :: [String] -> Opts
parseOpts = foldl applyOpt defaultOpts
  where
    applyOpt o line = case words line of
        ["dialect", d]   -> o { optDialect = parseShell d }
        ["exclude", c]   -> o { optExclude = read c : optExclude o }
        ["include", c]   -> o { optInclude = Just (read c : maybe [] id (optInclude o)) }
        ["severity", s]  -> o { optSeverity = parseSeverity s }
        ["enable", name] -> o { optEnable = name : optEnable o }
        ["extended-analysis"] -> o { optExtendedAnalysis = Just True }
        ["norc"]         -> o { optIgnoreRC = True }
        _                -> o  -- Silently ignore unknown options.

    parseShell "sh"      = Just Sh
    parseShell "bash"    = Just Bash
    parseShell "dash"    = Just Dash
    parseShell "ksh"     = Just Ksh
    parseShell "busybox" = Just BusyboxSh
    parseShell _         = Nothing

    parseSeverity "error"   = ErrorC
    parseSeverity "warning" = WarningC
    parseSeverity "info"    = InfoC
    parseSeverity _         = StyleC

buildSpec :: String -> Opts -> CheckSpec
buildSpec script opts = emptyCheckSpec
    { csFilename          = "-"
    , csScript            = script
    , csCheckSourced      = False
    , csIgnoreRC          = optIgnoreRC opts
    , csExcludedWarnings  = optExclude opts
    , csIncludedWarnings  = optInclude opts
    , csShellTypeOverride = optDialect opts
    , csMinSeverity       = optSeverity opts
    , csExtendedAnalysis  = optExtendedAnalysis opts
    , csOptionalChecks    = optEnable opts
    }

------------------------------------------------------------------------
-- JSON serialization (hand-rolled, no aeson)
------------------------------------------------------------------------

encodeResult :: CheckResult -> String
encodeResult cr =
    "{\"comments\":[" ++ intercalate "," (map encodeComment (crComments cr)) ++ "]}"

encodeComment :: PositionedComment -> String
encodeComment pc = concat
    [ "{"
    , "\"file\":"    , jsonString (posFile (pcStartPos pc)), ","
    , "\"line\":"    , show (posLine   (pcStartPos pc)),     ","
    , "\"column\":"  , show (posColumn (pcStartPos pc)),     ","
    , "\"endLine\":" , show (posLine   (pcEndPos pc)),       ","
    , "\"endColumn\":", show (posColumn (pcEndPos pc)),      ","
    , "\"level\":"   , jsonString (severityStr (cSeverity (pcComment pc))), ","
    , "\"code\":"    , show (cCode (pcComment pc)),          ","
    , "\"message\":" , jsonString (cMessage (pcComment pc)), ","
    , "\"fix\":"     , encodeFix (pcFix pc)
    , "}"
    ]

encodeFix :: Maybe Fix -> String
encodeFix Nothing  = "null"
encodeFix (Just f) =
    "{\"replacements\":[" ++ intercalate "," (map encodeReplacement (fixReplacements f)) ++ "]}"

encodeReplacement :: Replacement -> String
encodeReplacement r = concat
    [ "{"
    , "\"line\":"          , show (posLine   (repStartPos r)), ","
    , "\"column\":"        , show (posColumn (repStartPos r)), ","
    , "\"endLine\":"       , show (posLine   (repEndPos r)),   ","
    , "\"endColumn\":"     , show (posColumn (repEndPos r)),   ","
    , "\"precedence\":"    , show (repPrecedence r),           ","
    , "\"insertionPoint\":", jsonString (insertionPointStr (repInsertionPoint r)), ","
    , "\"replacement\":"   , jsonString (repString r)
    , "}"
    ]

severityStr :: Severity -> String
severityStr ErrorC   = "error"
severityStr WarningC = "warning"
severityStr InfoC    = "info"
severityStr StyleC   = "style"

insertionPointStr :: InsertionPoint -> String
insertionPointStr InsertBefore = "beforeStart"
insertionPointStr InsertAfter  = "afterEnd"

-- | Minimal JSON string encoder. Escapes backslash, double-quote,
-- and control characters (< 0x20).
jsonString :: String -> String
jsonString s = "\"" ++ concatMap escapeChar s ++ "\""
  where
    escapeChar '"'  = "\\\""
    escapeChar '\\' = "\\\\"
    escapeChar '\n' = "\\n"
    escapeChar '\r' = "\\r"
    escapeChar '\t' = "\\t"
    escapeChar c
        | c < ' '   = "\\u" ++ padHex 4 (fromEnum c)
        | otherwise  = [c]

    padHex width n =
        let hex = showHex' n
        in replicate (width - length hex) '0' ++ hex

    showHex' 0 = "0"
    showHex' n = go n []
      where
        go 0 acc = acc
        go x acc = go (x `div` 16) (intToDigit (x `mod` 16) : acc)
