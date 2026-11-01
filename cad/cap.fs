FeatureScript 3112;
import(path : "onshape/std/common.fs", version : "3112.0");

// The cap of the voistic stick, in place on top of the tube from the Voistic Tube feature, so both share one set of
// coordinates. Printed roof down. It plugs into the tube on its sides and back; the front stays open for the stack of
// boards. One plate runs down the back wall from the roof: full thickness to the land that presses on the USB connector
// and pushes the stack forward, then thinning to the tip, with a bump on its back that snaps into a dimple in the tube.
// The USB window is the last cut. It runs through the roof and the plate down to the mouth of the connector, which leaves
// a rib on each side of the plate, and the bottom of the cut slopes. With the roof on the bed nothing needs support.
annotation { "Feature Type Name" : "Voistic Cap", "Feature Type Description" : "Cap of the voice stick with a pressing plate" }
export const voisticCap = defineFeature(function(context is Context, id is Id, definition is map)
    precondition
    {
        annotation { "Name" : "Full length, with the cap" }
        isLength(definition.length, { (millimeter) : [20, 80, 300] } as LengthBoundSpec);
        annotation { "Name" : "Width" }
        isLength(definition.width, { (millimeter) : [10, 28, 100] } as LengthBoundSpec);
        annotation { "Name" : "Depth" }
        isLength(definition.depth, { (millimeter) : [5, 20, 100] } as LengthBoundSpec);
        annotation { "Name" : "Cap height, outside the tube" }
        isLength(definition.capHeight, { (millimeter) : [3, 6, 50] } as LengthBoundSpec);
        annotation { "Name" : "Tube wall" }
        isLength(definition.wall, { (millimeter) : [0.8, 1.8, 5] } as LengthBoundSpec);
        annotation { "Name" : "Long edge radius" }
        isLength(definition.edgeRadius, { (millimeter) : [1, 7, 30] } as LengthBoundSpec);
        annotation { "Name" : "Top chamfer" }
        isLength(definition.topChamfer, { (millimeter) : [0.2, 0.8, 5] } as LengthBoundSpec);
        annotation { "Name" : "Top radius" }
        isLength(definition.topRadius, { (millimeter) : [0.2, 3, 10] } as LengthBoundSpec);

        annotation { "Name" : "Roof" }
        isLength(definition.roof, { (millimeter) : [0.8, 2, 5] } as LengthBoundSpec);
        annotation { "Name" : "Plug into the tube, depth" }
        isLength(definition.spigot, { (millimeter) : [1, 4, 20] } as LengthBoundSpec);
        annotation { "Name" : "Fit in the tube, on a side" }
        isLength(definition.fit, { (millimeter) : [0, 0.15, 1] } as LengthBoundSpec);

        annotation { "Name" : "Board width" }
        isLength(definition.boardWidth, { (millimeter) : [5, 18, 40] } as LengthBoundSpec);
        annotation { "Name" : "Room for solder, on a side" }
        isLength(definition.sideGap, { (millimeter) : [0, 1.5, 5] } as LengthBoundSpec);
        annotation { "Name" : "Stack, button top to USB top" }
        isLength(definition.stackThickness, { (millimeter) : [5, 14.5, 30] } as LengthBoundSpec);
        annotation { "Name" : "Button above its square" }
        isLength(definition.buttonHead, { (millimeter) : [0, 1.5, 5] } as LengthBoundSpec);
        annotation { "Name" : "Square pocket depth in the tube" }
        isLength(definition.squarePocket, { (millimeter) : [0, 0.6, 3] } as LengthBoundSpec);
        annotation { "Name" : "USB connector height" }
        isLength(definition.usbHeight, { (millimeter) : [1, 3.2, 10] } as LengthBoundSpec);
        annotation { "Name" : "USB mouth from the top of the tube" }
        isLength(definition.usbMouthFromTop, { (millimeter) : [1, 6, 60] } as LengthBoundSpec);

        annotation { "Name" : "USB window width" }
        isLength(definition.usbWindowWidth, { (millimeter) : [5, 12, 26] } as LengthBoundSpec);
        annotation { "Name" : "USB window height" }
        isLength(definition.usbWindowHeight, { (millimeter) : [3, 7, 18] } as LengthBoundSpec);
        annotation { "Name" : "USB window corner radius" }
        isLength(definition.usbWindowRadius, { (millimeter) : [0.2, 3, 10] } as LengthBoundSpec);

        annotation { "Name" : "Plate width" }
        isLength(definition.plateWidth, { (millimeter) : [4, 14, 24] } as LengthBoundSpec);
        annotation { "Name" : "Plate tip thickness" }
        isLength(definition.plateTip, { (millimeter) : [0.5, 1.2, 4] } as LengthBoundSpec);
        annotation { "Name" : "Plate tip from the top of the tube" }
        isLength(definition.plateLength, { (millimeter) : [5, 20, 70] } as LengthBoundSpec);
        annotation { "Name" : "Pressing land below the USB mouth, length" }
        isLength(definition.landLength, { (millimeter) : [0.5, 3, 20] } as LengthBoundSpec);
        annotation { "Name" : "Plate curve radius" }
        isLength(definition.plateRadius, { (millimeter) : [0.5, 10, 50] } as LengthBoundSpec);
        annotation { "Name" : "Plate tip corner chamfer" }
        isLength(definition.tipChamfer, { (millimeter) : [0.1, 1, 5] } as LengthBoundSpec);
        annotation { "Name" : "Slope on the plug's side walls, length" }
        isLength(definition.spigotSlope, { (millimeter) : [1, 8, 20] } as LengthBoundSpec);

        annotation { "Name" : "Snap bump from the top of the tube" }
        isLength(definition.snapFromTop, { (millimeter) : [1, 18, 70] } as LengthBoundSpec);
        annotation { "Name" : "Snap bump height" }
        isLength(definition.snapHeight, { (millimeter) : [0.1, 0.5, 2] } as LengthBoundSpec);
        annotation { "Name" : "Snap bump diameter" }
        isLength(definition.snapDiameter, { (millimeter) : [0.5, 3, 10] } as LengthBoundSpec);
    }
    {
        const w = definition.width;
        const d = definition.depth;
        const t = definition.wall;
        const rc = definition.edgeRadius;
        const top = definition.length;
        const h = top - definition.capHeight;
        const tubeFront = -d / 2 + t;
        const tubeBack = d / 2 - t;

        // The stack against the front wall: its square sits in the pocket, so everything else follows from that
        const squareFace = tubeFront - definition.squarePocket;
        const usbBack = squareFace - definition.buttonHead + definition.stackThickness;
        const usbY = usbBack - definition.usbHeight / 2;

        const backY = tubeBack - definition.fit;
        const windowBack = usbY + definition.usbWindowHeight / 2;
        const roofZ = top - definition.roof;
        const snapZ = h - definition.snapFromTop;
        const mouthZ = h - definition.usbMouthFromTop;
        const landBottom = mouthZ - definition.landLength;
        const tipZ = h - definition.plateLength;
        const halfSide = definition.boardWidth / 2 + definition.sideGap;

        if (usbBack >= tubeBack)
            throw regenError("The stack does not fit in the tube", ["stackThickness"]);
        if (windowBack >= backY)
            throw regenError("The USB window leaves no root for the plate", ["usbWindowHeight"]);
        if (windowBack <= usbBack)
            throw regenError("The USB window does not reach past the connector", ["usbWindowHeight"]);
        if (definition.usbWindowRadius > definition.usbWindowHeight / 2 || definition.usbWindowRadius > definition.usbWindowWidth / 2)
            throw regenError("The window radius is too big for the window", ["usbWindowRadius"]);
        if (definition.roof >= definition.capHeight)
            throw regenError("The roof is thicker than the cap", ["roof"]);
        if (halfSide >= w / 2 - t - definition.fit)
            throw regenError("The boards leave no side walls for the plug", ["sideGap"]);
        if (landBottom <= tipZ)
            throw regenError("The plate ends before the pressing land", ["plateLength"]);
        if (definition.plateTip >= backY - usbBack)
            throw regenError("The plate tip is thicker than the gap behind the USB connector", ["plateTip"]);

        // Outside: the section of the tube, closed by the same chamfer and radius as the foot
        fCuboid(context, id + "outer", {
                    "corner1" : vector(-w / 2, -d / 2, h),
                    "corner2" : vector(w / 2, d / 2, top) });
        const body = qCreatedBy(id + "outer", EntityType.BODY);
        opFillet(context, id + "longEdges", {
                    "entities" : qParallelEdges(qOwnedByBody(body, EntityType.EDGE), vector(0, 0, 1)),
                    "radius" : rc });
        const topFace = qCoincidesWithPlane(qOwnedByBody(body, EntityType.FACE), plane(vector(0 * millimeter, 0 * millimeter, top), vector(0, 0, 1)));
        opChamfer(context, id + "topChamfer", {
                    "entities" : qAdjacent(topFace, AdjacencyType.EDGE, EntityType.EDGE),
                    "chamferType" : ChamferType.EQUAL_OFFSETS,
                    "width" : definition.topChamfer });
        opFillet(context, id + "topRadius", {
                    "entities" : qCoincidesWithPlane(qOwnedByBody(body, EntityType.EDGE),
                            plane(vector(0 * millimeter, 0 * millimeter, top - definition.topChamfer), vector(0, 0, 1))),
                    "radius" : definition.topRadius });

        // The plug: the inside of the tube less the fit
        const fw = w / 2 - t - definition.fit;
        const fd = d / 2 - t - definition.fit;
        fCuboid(context, id + "spigot", {
                    "corner1" : vector(-fw, -fd, h - definition.spigot),
                    "corner2" : vector(fw, fd, h + 1 * millimeter) });
        const spigot = qCreatedBy(id + "spigot", EntityType.BODY);
        opFillet(context, id + "spigotEdges", {
                    "entities" : qParallelEdges(qOwnedByBody(spigot, EntityType.EDGE), vector(0, 0, 1)),
                    "radius" : rc - t - definition.fit });
        opBoolean(context, id + "join", {
                    "tools" : qUnion([body, spigot]),
                    "operationType" : BooleanOperationType.UNION });

        // Room for the stack, open to the front, so the plug is left on the sides and the back
        fCuboid(context, id + "room", {
                    "corner1" : vector(-halfSide, squareFace, h - definition.spigot - 1 * millimeter),
                    "corner2" : vector(halfSide, windowBack, roofZ) });
        const room = qCreatedBy(id + "room", EntityType.BODY);
        opFillet(context, id + "roomEdges", {
                    "entities" : qParallelEdges(qOwnedByBody(room, EntityType.EDGE), vector(0, 0, 1)),
                    "radius" : 3 * millimeter });
        opBoolean(context, id + "cutRoom", {
                    "tools" : room,
                    "targets" : body,
                    "operationType" : BooleanOperationType.SUBTRACTION });

        // The side walls of the plug slope away towards the front, so they end in no sharp points
        const yz = function(name is string, x is ValueWithUnits, width is ValueWithUnits, points is array)
            {
                const sketch = newSketchOnPlane(context, id + (name ~ "Sketch"), {
                            "sketchPlane" : plane(vector(x, 0 * millimeter, 0 * millimeter), vector(1, 0, 0), vector(0, 1, 0)) });
                skPolyline(sketch, "outline", { "points" : append(points, points[0]) });
                skSolve(sketch);
                opExtrude(context, id + name, {
                            "entities" : qSketchRegion(id + (name ~ "Sketch")),
                            "direction" : vector(1, 0, 0),
                            "endBound" : BoundingType.BLIND,
                            "endDepth" : width });
                opDeleteBodies(context, id + (name ~ "SketchGone"), { "entities" : qCreatedBy(id + (name ~ "Sketch"), EntityType.BODY) });
                return qCreatedBy(id + name, EntityType.BODY);
            };
        const slopeFront = tubeFront - 1 * millimeter;
        const slopeRun = definition.spigotSlope + 1 * millimeter;
        const slopeBottom = h - definition.spigot - 1 * millimeter;
        opBoolean(context, id + "cutSlope", {
                    "tools" : yz("slope", -w / 2, w, [
                            vector(slopeFront, h),
                            vector(slopeFront, slopeBottom),
                            vector(slopeFront + slopeRun * (definition.spigot + 1 * millimeter) / definition.spigot, slopeBottom)]),
                    "targets" : body,
                    "operationType" : BooleanOperationType.SUBTRACTION });

        // The plate: from the roof to the land on the USB connector at full thickness, then down to the tip
        const pw = definition.plateWidth;
        const plate = yz("plate", -pw / 2, pw, [
                vector(backY, roofZ + 0.5 * millimeter),
                vector(usbBack, roofZ + 0.5 * millimeter),
                vector(usbBack, landBottom),
                vector(backY - definition.plateTip, tipZ),
                vector(backY, tipZ)]);
        const plateEdges = qCreatedBy(id + "plate", EntityType.EDGE);
        const tipY = backY - definition.plateTip / 2;
        opChamfer(context, id + "tipCorners", {
                    "entities" : qUnion([
                                qContainsPoint(plateEdges, vector(-pw / 2, tipY, tipZ)),
                                qContainsPoint(plateEdges, vector(pw / 2, tipY, tipZ))]),
                    "chamferType" : ChamferType.EQUAL_OFFSETS,
                    "width" : definition.tipChamfer });
        opFillet(context, id + "plateRadius", {
                    "entities" : qContainsPoint(plateEdges, vector(0 * millimeter, usbBack, landBottom)),
                    "radius" : definition.plateRadius });

        // The bump: a cap of a sphere on the back of the plate
        const snapHalf = definition.snapDiameter / 2;
        const snapR = (snapHalf * snapHalf + definition.snapHeight * definition.snapHeight) / (2 * definition.snapHeight);
        opPoint(context, id + "snapCentre", { "point" : vector(0 * millimeter, backY + definition.snapHeight - snapR, snapZ) });
        fSphere(context, id + "snap", { "center" : qCreatedBy(id + "snapCentre", EntityType.VERTEX), "radius" : snapR });
        opDeleteBodies(context, id + "snapCentreGone", { "entities" : qCreatedBy(id + "snapCentre", EntityType.BODY) });
        const snap = qCreatedBy(id + "snap", EntityType.BODY);
        fCuboid(context, id + "snapTrim", {
                    "corner1" : vector(-snapR - 1 * millimeter, backY - snapR - snapR, snapZ - snapR - 1 * millimeter),
                    "corner2" : vector(snapR + 1 * millimeter, backY - 0.2 * millimeter, snapZ + snapR + 1 * millimeter) });
        opBoolean(context, id + "cutSnap", {
                    "tools" : qCreatedBy(id + "snapTrim", EntityType.BODY),
                    "targets" : snap,
                    "operationType" : BooleanOperationType.SUBTRACTION });
        opBoolean(context, id + "joinPlate", {
                    "tools" : qUnion([body, plate, snap]),
                    "operationType" : BooleanOperationType.UNION });

        // Last, the USB window, built from pieces so that a radius of half the height gives a full round end
        const halfW = definition.usbWindowWidth / 2;
        const halfH = definition.usbWindowHeight / 2;
        const r = definition.usbWindowRadius;
        const cutTo = top + 1 * millimeter;
        fCuboid(context, id + "window", {
                    "corner1" : vector(-halfW + r, usbY - halfH, mouthZ),
                    "corner2" : vector(halfW - r, usbY + halfH, cutTo) });
        const window = qCreatedBy(id + "window", EntityType.BODY);
        var pieces = [window];
        if (r < halfH)
        {
            fCuboid(context, id + "windowTall", {
                        "corner1" : vector(-halfW, usbY - halfH + r, mouthZ),
                        "corner2" : vector(halfW, usbY + halfH - r, cutTo) });
            pieces = append(pieces, qCreatedBy(id + "windowTall", EntityType.BODY));
        }
        var corner = 0;
        for (var sx in [-1, 1])
        {
            for (var sy in [-1, 1])
            {
                const x = sx * (halfW - r);
                const y = usbY + sy * (halfH - r);
                fCylinder(context, id + ("windowCorner" ~ corner), {
                            "bottomCenter" : vector(x, y, mouthZ),
                            "topCenter" : vector(x, y, cutTo),
                            "radius" : r });
                pieces = append(pieces, qCreatedBy(id + ("windowCorner" ~ corner), EntityType.BODY));
                corner += 1;
            }
        }
        opBoolean(context, id + "joinWindow", {
                    "tools" : qUnion(pieces),
                    "operationType" : BooleanOperationType.UNION });
        opChamfer(context, id + "windowSlope", {
                    "entities" : qCoincidesWithPlane(qOwnedByBody(window, EntityType.EDGE), plane(vector(0 * millimeter, 0 * millimeter, mouthZ), vector(0, 0, 1))),
                    "chamferType" : ChamferType.EQUAL_OFFSETS,
                    "width" : windowBack - usbBack });
        opBoolean(context, id + "cutWindow", {
                    "tools" : window,
                    "targets" : body,
                    "operationType" : BooleanOperationType.SUBTRACTION });
    }, {
        "length" : 80 * millimeter,
        "width" : 28 * millimeter,
        "depth" : 20 * millimeter,
        "capHeight" : 6 * millimeter,
        "wall" : 1.8 * millimeter,
        "edgeRadius" : 7 * millimeter,
        "topChamfer" : 0.8 * millimeter,
        "topRadius" : 3 * millimeter,
        "roof" : 2 * millimeter,
        "spigot" : 4 * millimeter,
        "fit" : 0.15 * millimeter,
        "boardWidth" : 18 * millimeter,
        "sideGap" : 1.5 * millimeter,
        "stackThickness" : 14.5 * millimeter,
        "buttonHead" : 1.5 * millimeter,
        "squarePocket" : 0.6 * millimeter,
        "usbHeight" : 3.2 * millimeter,
        "usbMouthFromTop" : 6 * millimeter,
        "usbWindowWidth" : 12 * millimeter,
        "usbWindowHeight" : 7 * millimeter,
        "usbWindowRadius" : 3 * millimeter,
        "plateWidth" : 14 * millimeter,
        "plateTip" : 1.2 * millimeter,
        "plateLength" : 20 * millimeter,
        "landLength" : 3 * millimeter,
        "plateRadius" : 10 * millimeter,
        "tipChamfer" : 1 * millimeter,
        "spigotSlope" : 8 * millimeter,
        "snapFromTop" : 18 * millimeter,
        "snapHeight" : 0.5 * millimeter,
        "snapDiameter" : 3 * millimeter
    });
